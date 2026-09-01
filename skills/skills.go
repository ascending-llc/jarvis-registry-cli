package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ascending-llc/jarvis-registry-cli/auth"
	"github.com/ascending-llc/jarvis-registry-cli/cfg"
)

type (
	// Logger is the minimal logging interface required by this package,
	// satisfied by *log.Logger.
	Logger interface {
		Print(v ...any)
		Printf(format string, v ...any)
		Println(v ...any)
	}

	// TokenProvider resolves a Registry access token for SyncCommand.
	TokenProvider interface {
		GetAccessToken() (string, error)
	}

	// SyncCommand implements the "sync-skills" subcommand: it reconciles
	// the local skills folder against the skills available to the
	// caller on the Registry, creating, updating, and deleting local
	// skill folders as needed.
	SyncCommand struct {
		userHomeDir    string
		registryDir    string
		destDir        string
		tempDir        string
		baseUrl        string
		logger         Logger
		configLoadFunc func(string) (cfg.Config, error)
		tp             TokenProvider
		client         Client
		mrw            ManifestReadWriter
	}

	// SyncSpec describes one skill's local and remote state, as compared
	// by SyncCommand to decide whether to create, update, or delete it.
	SyncSpec struct {
		Id            string
		LocalName     string
		RemoteName    string
		LocalVersion  int
		RemoteVersion int
	}

	// SyncFn creates or updates the local copy of the skill described by
	// spec and returns its resulting metadata. An update implementation may
	// only stage the change rather than persist it directly; see stageOne
	// and commitStaged.
	SyncFn func(SyncSpec) (Metadata, error)
)

const (
	skillsReadScope = "skills-read"

	registryBasePath = "/gateway"

	registryDirName = ".jarvis-registry"

	concurrency = 5

	// tempDirPattern names the scratch directory stageOne stages updated
	// skills into. It is created inside destDir (see MkdirTemp's dir
	// argument in Run) so that commitStaged's move into destDir is a same-
	// filesystem, single-syscall os.Rename rather than a cross-device copy.
	tempDirPattern = ".jarvis-registry-sync-*"
)

// BeforeReset sets defaults for SyncCommand that don't depend on parsed
// flags: the user's home directory, the config loader, and the logger.
func (c *SyncCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load

	c.logger = log.New(os.Stdout, "jarvis-registry", 0)

	return nil
}

// AfterApply derives SyncCommand's remaining dependencies from the loaded
// config: the registry directory, destination folder, Registry base URL,
// and token provider.
func (c *SyncCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, registryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.destDir = config.Local.Dest

	c.baseUrl = config.Registry.BaseUrl

	c.tp = auth.NewRegistryTokenResolver(c.baseUrl, []string{skillsReadScope}, c.logger)

	return nil
}

// Run resolves a Registry access token, then reconciles the local skills
// folder against the Registry: skills no longer accessible are deleted,
// existing skills are updated in place, and new skills are created,
// before the sync manifest is rewritten to reflect the new state. A skill
// that individually fails to sync (e.g. its remote content has
// unreconcilable frontmatter) does not prevent any other skill in the same
// run from syncing: Run still persists every skill that succeeded to the
// manifest, but returns a non-nil error naming the ones that failed, so the
// command's own exit code/output is the only signal of a partial
// failure — nothing is silently swallowed.
func (c *SyncCommand) Run() (err error) {
	// initialize the final two dependencies c.client and c.mrw
	token, err := c.tp.GetAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get Registry access token: %s", err.Error())
	}

	if c.client, err = NewClient(c.baseUrl+registryBasePath, token); err != nil {
		return fmt.Errorf("failed to create Registry client: %s", err.Error())
	}

	c.mrw = NewManifestReadWriter(c.registryDir)

	// make sure the destination folder exists
	if err = c.guaranteeDestDir(); err != nil {
		return err
	}

	// gather local skills metadata
	localSkills, err := c.mrw.ReadSkills()
	if err != nil {
		return err
	}

	// clean destination folder according to manifest file — only those in the manifest file are retained
	if err = c.cleanDestDir(localSkills); err != nil {
		return fmt.Errorf("failed to clean up the skills folder before syncing: %s", err.Error())
	}

	// gather remote skills metadata
	remoteSkills, err := c.client.ListSkills()
	if err != nil {
		return err
	}

	// reject any remote skill name that is unsafe to use as a filesystem
	// path component before it reaches any os.* call
	for _, r := range remoteSkills {
		if !isSafeSkillName(r.Name) {
			return fmt.Errorf("remote skill %s (id %s) has a name that is unsafe to use as a filesystem path", r.Name, r.Id)
		}
	}

	// compare local and remote and categorize skills
	toDelete, toUpdate, toCreate := c.getSyncSpecs(localSkills, remoteSkills)

	// MUST do delete->update->create sequentially. This is to avoid a remote skill's name colliding with a local skill's outdated name,
	// because skill name is bound to a local folder name. Once toDelete are deleted and toUpdate get folder names updated to
	// the remote skill names, the server guarantees that there is no name conflict among remote skills returned by ListSkills.
	if err = c.deleteMany(toDelete); err != nil {
		return fmt.Errorf("failed to delete certain skills that user no longer has access to: %s", err.Error())
	}

	// stageOne (fanned out below) never writes into destDir under a skill's
	// RemoteName directly; it stages that content here instead. This keeps
	// concurrent stageOne calls collision-free even when two specs swap
	// names, since commitStaged only moves staged folders into destDir
	// once every stageOne call — and every RemoveAll of an outdated
	// LocalName — has already finished.
	if c.tempDir, err = os.MkdirTemp(c.destDir, tempDirPattern); err != nil {
		return fmt.Errorf("failed to create scratch directory for staging updated skills: %s", err.Error())
	}

	defer func() { _ = os.RemoveAll(c.tempDir) }()

	updatedSkills, updateErr := c.boundedFanOut(toUpdate, c.stageOne)

	if err = c.commitStaged(toUpdate); err != nil {
		return fmt.Errorf("failed to finalize updated skills: %s", err.Error())
	}

	createdSkills, createErr := c.boundedFanOut(toCreate, c.createOne)

	// write the new manifest file with whatever succeeded; a per-skill
	// failure (e.g. invalid frontmatter) must not prevent every other skill
	// in this run from being synced and recorded.
	succeeded := succeededOnly(slices.Concat(updatedSkills, createdSkills))
	if err = c.mrw.WriteManifest(succeeded); err != nil {
		return fmt.Errorf("failed to write manifest file after syncing: %s", err.Error())
	}

	return errors.Join(updateErr, createErr)
}

// succeededOnly filters out the zero-value Metadata entries boundedFanOut
// leaves at the index of any spec whose SyncFn call returned an error.
func succeededOnly(skills []Metadata) []Metadata {
	result := make([]Metadata, 0, len(skills))

	for _, s := range skills {
		if s.Id != "" {
			result = append(result, s)
		}
	}

	return result
}

// isSafeSkillName reports whether name can be safely used as a single
// filesystem path component under destDir. filepath.Join only lexically
// normalizes its arguments; it does not confine the result to destDir, so
// a name such as "../../etc" must be rejected explicitly rather than
// relied upon to be cleaned away.
func isSafeSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}

	return !strings.ContainsAny(name, `/\`)
}

func (c *SyncCommand) guaranteeDestDir() error {
	if stat, err := os.Stat(c.destDir); errors.Is(err, fs.ErrNotExist) {
		if err = os.MkdirAll(c.destDir, 0755); err != nil {
			return fmt.Errorf("failed to create skills folder at %s: %s", c.destDir, err.Error())
		}
	} else if err != nil {
		return fmt.Errorf("skills folder %s already exists but cannot be queried for stat: %s", c.destDir, err.Error())
	} else if !stat.IsDir() {
		return fmt.Errorf("intended skills folder %s is already a file", c.destDir)
	}

	return nil
}

func (c *SyncCommand) cleanDestDir(skills []Metadata) error {
	entries, err := os.ReadDir(c.destDir)
	if err != nil {
		return fmt.Errorf("failed to list contents of the skills folder at %s: %s", c.destDir, err.Error())
	}

	toRemove := make(map[string]struct{}, len(entries))

	for _, e := range entries {
		toRemove[e.Name()] = struct{}{}
	}

	for _, s := range skills {
		delete(toRemove, s.Name)
	}

	errs := make([]error, 0, len(toRemove))

	for base := range toRemove {
		errs = append(errs, os.RemoveAll(filepath.Join(c.destDir, base)))
	}

	return errors.Join(errs...)
}

func (c *SyncCommand) getSyncSpecs(local, remote []Metadata) (toDelete []SyncSpec, toUpdate []SyncSpec, toCreate []SyncSpec) {
	var (
		localSet  = make(map[string]Metadata)
		remoteSet = make(map[string]Metadata)
	)

	var (
		id   string
		ok   bool
		l, r Metadata
	)

	for _, l = range local {
		localSet[l.Id] = l
	}

	for _, r = range remote {
		remoteSet[r.Id] = r
	}

	for id, l = range localSet {
		if r, ok = remoteSet[id]; ok {
			toUpdate = append(toUpdate, SyncSpec{Id: id, LocalName: l.Name, LocalVersion: l.Version, RemoteName: r.Name, RemoteVersion: r.Version})
		} else {
			toDelete = append(toDelete, SyncSpec{Id: id, LocalName: l.Name, LocalVersion: l.Version, RemoteName: "", RemoteVersion: 0})
		}
	}

	for id, r = range remoteSet {
		if _, ok = localSet[id]; !ok {
			toCreate = append(toCreate, SyncSpec{Id: id, LocalName: "", LocalVersion: 0, RemoteName: r.Name, RemoteVersion: r.Version})
		}
	}

	return toDelete, toUpdate, toCreate
}

func (c *SyncCommand) createOne(spec SyncSpec) (Metadata, error) {
	content, err := c.client.GetSkillContent(spec.Id)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to retrieve contents for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	rendered, err := renderSkillMarkdown(content, spec.RemoteName)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to render SKILL.md for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.Mkdir(filepath.Join(c.destDir, spec.RemoteName), 0755); err != nil {
		return Metadata{}, fmt.Errorf("failed to create folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.WriteFile(filepath.Join(c.destDir, spec.RemoteName, "SKILL.md"), []byte(rendered), 0644); err != nil {
		return Metadata{}, fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	return Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, nil
}

// stageOne prepares an up-to-date local copy of a skill that exists both
// locally and remotely, ahead of commitStaged moving it into place. It
// re-fetches the skill's content whenever the local name or version
// differs from the remote, or when the expected local folder is missing,
// unreadable, or not a directory — treating any such stat anomaly as
// needing a refresh; otherwise it is a no-op. When a refresh is needed, it
// removes the outdated folder under LocalName from destDir directly, but
// builds the fresh folder under RemoteName inside c.tempDir rather than
// destDir: RemoteName is guaranteed unique across a ListSkills response,
// so concurrent stageOne calls can never collide with each other there,
// even when two specs in the same batch swap names (A: foo→bar, B:
// bar→foo) — unlike destDir, where A's fresh "bar" and B's outdated "bar"
// would be the same path.
func (c *SyncCommand) stageOne(spec SyncSpec) (Metadata, error) {
	var needChange bool

	if stat, err := os.Stat(filepath.Join(c.destDir, spec.LocalName)); errors.Is(err, fs.ErrNotExist) {
		needChange = true
	} else if err != nil {
		needChange = true
	} else if !stat.IsDir() {
		needChange = true
	} else {
		needChange = (spec.LocalName != spec.RemoteName || spec.LocalVersion != spec.RemoteVersion)
	}

	if !needChange {
		return Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, nil
	}

	content, err := c.client.GetSkillContent(spec.Id)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to retrieve contents for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	rendered, err := renderSkillMarkdown(content, spec.RemoteName)
	if err != nil {
		return Metadata{}, fmt.Errorf("failed to render SKILL.md for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.RemoveAll(filepath.Join(c.destDir, spec.LocalName)); err != nil {
		return Metadata{}, fmt.Errorf("failed to remove outdated local skill %s, version %d: %s", spec.LocalName, spec.LocalVersion, err.Error())
	}

	if err = os.Mkdir(filepath.Join(c.tempDir, spec.RemoteName), 0755); err != nil {
		return Metadata{}, fmt.Errorf("failed to stage folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.WriteFile(filepath.Join(c.tempDir, spec.RemoteName, "SKILL.md"), []byte(rendered), 0644); err != nil {
		return Metadata{}, fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	return Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, nil
}

// commitStaged moves every folder stageOne staged under c.tempDir into
// destDir under its final RemoteName. Callers must run this only after
// every stageOne call for specs has returned — including every RemoveAll
// of an outdated LocalName — so that no move performed here can still be
// followed by a sibling spec's delete of that same destination path. A
// spec that was a no-op in stageOne has nothing staged and is skipped.
// Moving is a single same-filesystem os.Rename per spec (see tempDirPattern),
// cheap enough that fanning it out concurrently would not be worthwhile.
func (c *SyncCommand) commitStaged(specs []SyncSpec) error {
	errs := make([]error, 0, len(specs))

	for _, spec := range specs {
		staged := filepath.Join(c.tempDir, spec.RemoteName)

		if err := os.Rename(staged, filepath.Join(c.destDir, spec.RemoteName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("failed to move staged skill %s, version %d into place: %s", spec.RemoteName, spec.RemoteVersion, err.Error()))
		}
	}

	return errors.Join(errs...)
}

// boundedFanOut calls fn once per spec, running at most `concurrency`
// calls at a time, and returns each call's result at the same index as
// its spec, alongside every per-call error joined together.
func (c *SyncCommand) boundedFanOut(specs []SyncSpec, fn SyncFn) ([]Metadata, error) {
	results := make([]Metadata, len(specs))
	errs := make([]error, len(specs))

	var wg sync.WaitGroup

	semaphore := make(chan struct{}, concurrency)

	for i, spec := range specs {
		semaphore <- struct{}{}

		wg.Add(1)

		go func(i int, spec SyncSpec) {
			defer wg.Done()

			defer func() { <-semaphore }()

			results[i], errs[i] = fn(spec)
		}(i, spec)
	}

	wg.Wait()

	return results, errors.Join(errs...)
}

func (c *SyncCommand) deleteMany(specs []SyncSpec) error {
	errs := make([]error, len(specs))

	var wg sync.WaitGroup

	semaphore := make(chan struct{}, concurrency)

	for i, spec := range specs {
		semaphore <- struct{}{}

		wg.Add(1)

		go func(i int, spec SyncSpec) {
			defer wg.Done()

			defer func() { <-semaphore }()

			errs[i] = os.RemoveAll(filepath.Join(c.destDir, spec.LocalName))
		}(i, spec)
	}

	wg.Wait()

	return errors.Join(errs...)
}
