package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"

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
		logger         Logger
		stdin          io.Reader
		tp             TokenProvider
		stderrLogger   Logger
		isTerminal     func() bool
		configLoadFunc func(string) (cfg.Config, error)
		client         Client
		destDir        string
		authBaseUrl    string
		baseUrl        string
		tempDir        string
		userHomeDir    string
		mrw            ManifestReadWriter
		pluginRoot     string
		registryDir    string
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
	// spec and returns a SyncOutcome describing what happened. An update
	// implementation may only stage the change rather than persist it
	// directly; see stageOne and commitStaged.
	SyncFn func(SyncSpec) SyncOutcome

	// SyncOutcome is the result of one SyncFn call against a single
	// SyncSpec: the resulting skill metadata, whether the local copy was
	// actually created or modified on disk, and any error encountered.
	// Changed is false only when stageOne determined the on-disk copy
	// already matched spec and left it untouched. Metadata is the zero
	// value whenever Err is non-nil, and Changed is meaningless (left
	// false) for outcomes — such as deleteMany's — that never populate
	// Metadata to begin with.
	SyncOutcome struct {
		Err      error
		Metadata Metadata
		Changed  bool
	}

	// summaryRow is one row of the sync summary table Run prints when it
	// finishes.
	summaryRow struct {
		Skill    string
		Status   string
		Previous string
		Current  string
		Notes    string
	}
)

const (
	registryBasePath = "/gateway"

	concurrency = 5

	// tempDirPattern names the scratch directory stageOne and createOne
	// stage, respectively, updated and newly created skills into. It is
	// created inside destDir (see MkdirTemp's dir argument in Run) so that
	// moving a staged folder into destDir is a same-filesystem, single-
	// syscall os.Rename rather than a cross-device copy.
	tempDirPattern = ".jarvis-registry-sync-*"

	statusCreated   = "Created"
	statusUpdated   = "Updated"
	statusUnchanged = "Unchanged"
	statusRemoved   = "Removed"
	statusFailed    = "Failed"

	// recreatedNote is the Notes value for an Updated summary row whose
	// recorded LocalVersion and RemoteVersion already agreed — Changed is
	// true only because stageOne found the local folder missing,
	// unreadable, or not a directory and recreated it.
	recreatedNote = "local copy was missing, unreadable, or not a directory; recreated"
)

// BeforeReset sets defaults for SyncCommand that don't depend on parsed
// flags: the user's home directory, the config loader, the stdout/stderr
// loggers, and the consent gate's terminal-detection and stdin source.
func (c *SyncCommand) BeforeReset() (err error) {
	if c.userHomeDir, err = os.UserHomeDir(); err != nil {
		return fmt.Errorf("could not locate user home directory: %s", err.Error())
	}

	c.configLoadFunc = cfg.Load

	c.logger = log.New(os.Stdout, "", 0)

	c.stderrLogger = log.New(os.Stderr, "", 0)

	c.isTerminal = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }

	c.stdin = os.Stdin

	return nil
}

// AfterApply derives SyncCommand's remaining dependencies from the loaded
// config: the registry directory, plugin root, destination folder,
// Registry and auth-server base URLs, and token provider.
func (c *SyncCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	c.pluginRoot = config.Local.PluginRoot

	c.destDir = config.Local.Dest

	c.baseUrl = config.Registry.BaseUrl

	c.authBaseUrl = config.Registry.AuthBaseUrl

	c.tp = auth.NewRegistryTokenResolver(c.authBaseUrl, auth.RegistryScopes, c.logger)

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

	c.mrw = NewManifestReadWriter(c.pluginRoot)

	// acquire the advisory lock for this plugin root before touching the
	// filesystem at all, so two concurrent invocations against the same
	// target can't race on the consent check or the bootstrap writes
	release, err := acquireLock(c.registryDir, c.pluginRoot)
	if err != nil {
		return err
	}

	defer release()

	// gate any mutation of a pre-existing, possibly foreign plugin root
	if err = c.ensurePluginRootConsent(); err != nil {
		return err
	}

	// consent already granted: create the plugin root if it doesn't exist yet
	if err = os.MkdirAll(c.pluginRoot, 0755); err != nil {
		return fmt.Errorf("failed to create plugin root at %s: %s", c.pluginRoot, err.Error())
	}

	// reconcile the CLI-owned plugin manifest
	pluginJSONCreated, err := reconcilePluginManifest(c.pluginRoot, c.stderrLogger)
	if err != nil {
		return err
	}

	// make sure the destination folder exists
	if err = c.guaranteeDestDir(); err != nil {
		return err
	}

	// gather local skills metadata
	manifest, err := c.mrw.ReadManifest()
	if err != nil {
		return err
	}

	localSkills := manifest.Skills

	// reconcile the CLI-owned sync-skills wrapper skill
	newSyncSkillsVersion, err := reconcileSyncSkillsWrapper(c.destDir, manifest.SyncSkillsVersion, c.stderrLogger)
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
	// path component, that would corrupt the Markdown sync summary table,
	// or that collides with this CLI's own reserved wrapper skill name,
	// before it reaches any os.* call
	for _, r := range remoteSkills {
		if !isSafeSkillName(r.Name) {
			return fmt.Errorf("remote skill %s (id %s) has a name that is unsafe to use as a filesystem path or in the sync summary table", r.Name, r.Id)
		}

		if strings.EqualFold(r.Name, reservedSyncSkillsName) {
			return fmt.Errorf("remote skill %s (id %s) is named %q, which is reserved for this CLI's own wrapper skill", r.Name, r.Id, reservedSyncSkillsName)
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
	// once every stageOne call — and every atomicRemoveAll of an outdated
	// LocalName — has already finished.
	if c.tempDir, err = os.MkdirTemp(c.destDir, tempDirPattern); err != nil {
		return fmt.Errorf("failed to create scratch directory for staging updated skills: %s", err.Error())
	}

	defer func() { _ = os.RemoveAll(c.tempDir) }()

	updateOutcomes := c.boundedFanOut(toUpdate, c.stageOne)

	if err = c.commitStaged(toUpdate); err != nil {
		return fmt.Errorf("failed to finalize updated skills: %s", err.Error())
	}

	createOutcomes := c.boundedFanOut(toCreate, c.createOne)

	// write the new manifest file with whatever succeeded; a per-skill
	// failure (e.g. invalid frontmatter) must not prevent every other skill
	// in this run from being synced and recorded.
	succeeded := succeededOnly(slices.Concat(updateOutcomes, createOutcomes))
	if err = c.mrw.WriteManifest(succeeded, newSyncSkillsVersion); err != nil {
		return fmt.Errorf("failed to write manifest file after syncing: %s", err.Error())
	}

	c.printSummary(pluginJSONCreated, toCreate, createOutcomes, toUpdate, updateOutcomes, toDelete)

	return errors.Join(joinErrors(updateOutcomes), joinErrors(createOutcomes))
}

// succeededOnly returns the Metadata of every outcome whose SyncFn call
// completed without error.
func succeededOnly(outcomes []SyncOutcome) []Metadata {
	result := make([]Metadata, 0, len(outcomes))

	for _, o := range outcomes {
		if o.Err == nil {
			result = append(result, o.Metadata)
		}
	}

	return result
}

// joinErrors aggregates every non-nil error carried by outcomes into a
// single error, as boundedFanOut itself no longer does.
func joinErrors(outcomes []SyncOutcome) error {
	errs := make([]error, len(outcomes))

	for i, o := range outcomes {
		errs[i] = o.Err
	}

	return errors.Join(errs...)
}

// isSafeSkillName reports whether name can be safely used both as a single
// filesystem path component under destDir and as a Skill cell in the
// Markdown sync summary table (see printSummary). filepath.Join only
// lexically normalizes its arguments; it does not confine the result to
// destDir, so a name such as "../../etc" must be rejected explicitly
// rather than relied upon to be cleaned away. "|" is rejected because the
// summary table's Markdown renderer does not escape it: an unescaped "|"
// in a Skill cell corrupts the table's column structure.
func isSafeSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}

	return !strings.ContainsAny(name, `/\|`)
}

// guaranteeDestDir ensures c.destDir exists as a directory, creating it
// if necessary.
func (c *SyncCommand) guaranteeDestDir() error {
	if stat, statErr := os.Stat(c.destDir); errors.Is(statErr, fs.ErrNotExist) {
		if err := os.MkdirAll(c.destDir, 0755); err != nil {
			return fmt.Errorf("failed to create skills folder at %s: %s", c.destDir, err.Error())
		}

		return nil
	} else if statErr != nil {
		return fmt.Errorf("skills folder %s already exists but cannot be queried for stat: %s", c.destDir, statErr.Error())
	} else if !stat.IsDir() {
		return fmt.Errorf("intended skills folder %s is already a file", c.destDir)
	}

	return nil
}

// atomicRemoveAll removes the directory tree at path by first renaming it
// to a sibling trash name and only then recursively removing the trash
// copy. os.RemoveAll on a directory is a multi-syscall tree walk that can
// leave a partially-emptied directory at its original, still-visible name
// if interrupted; renaming first is a single, same-filesystem syscall, so
// from any external observer's point of view path either fully exists
// under its real name or is already gone. A leftover trash entry from an
// interrupted RemoveAll is swept up by the next cleanDestDir run.
// atomicRemoveAll reports no error when path does not exist.
func atomicRemoveAll(path string) error {
	trash := filepath.Join(filepath.Dir(path), fmt.Sprintf(".trash-%s-%d", filepath.Base(path), time.Now().UnixNano()))

	if err := os.Rename(path, trash); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return err
	}

	return os.RemoveAll(trash)
}

// cleanDestDir removes every entry directly under c.destDir that isn't
// named in skills, except reservedSyncSkillsName — the CLI-owned
// sync-skills/ wrapper folder is never tracked in the manifest, but must
// never be deleted regardless of manifest content.
func (c *SyncCommand) cleanDestDir(skills []Metadata) error {
	entries, err := os.ReadDir(c.destDir)
	if err != nil {
		return fmt.Errorf("failed to list contents of the skills folder at %s: %s", c.destDir, err.Error())
	}

	toRemove := make(map[string]struct{}, len(entries))

	for _, e := range entries {
		if strings.EqualFold(e.Name(), reservedSyncSkillsName) {
			continue
		}

		toRemove[e.Name()] = struct{}{}
	}

	for _, s := range skills {
		delete(toRemove, s.Name)
	}

	errs := make([]error, 0, len(toRemove))

	for base := range toRemove {
		errs = append(errs, atomicRemoveAll(filepath.Join(c.destDir, base)))
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

// createOne fetches, renders, and stages a brand-new skill under
// c.tempDir/RemoteName, moving it into destDir only as the last step —
// mirroring stageOne's staging pattern — so a crash partway through never
// leaves a visible, half-created skill folder under destDir. RemoteName
// is guaranteed unique across a ListSkills response, and createOne's
// fan-out only starts after commitStaged has drained every update-staged
// folder out of c.tempDir, so concurrent createOne calls can never
// collide with each other or with an in-flight stageOne call there.
func (c *SyncCommand) createOne(spec SyncSpec) SyncOutcome {
	content, err := c.client.GetSkillContent(spec.Id)
	if err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to retrieve contents for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	rendered, err := renderSkillMarkdown(content, spec.RemoteName)
	if err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to render SKILL.md for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = os.Mkdir(filepath.Join(c.tempDir, spec.RemoteName), 0755); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to stage folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = os.WriteFile(filepath.Join(c.tempDir, spec.RemoteName, "SKILL.md"), []byte(rendered), 0644); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = os.Rename(filepath.Join(c.tempDir, spec.RemoteName), filepath.Join(c.destDir, spec.RemoteName)); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to move staged skill %s, version %d into place: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	return SyncOutcome{Metadata: Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, Changed: true}
}

// stageOne prepares an up-to-date local copy of a skill that exists both
// locally and remotely, ahead of commitStaged moving it into place. It
// re-fetches the skill's content whenever the local name or version
// differs from the remote, or when the expected local folder is missing,
// unreadable, or not a directory — treating any such stat anomaly as
// needing a refresh; otherwise it is a no-op. When a refresh is needed, it
// builds the fresh folder under RemoteName inside c.tempDir rather than
// destDir — RemoteName is guaranteed unique across a ListSkills response,
// so concurrent stageOne calls can never collide with each other there,
// even when two specs in the same batch swap names (A: foo→bar, B:
// bar→foo), unlike destDir, where A's fresh "bar" and B's outdated "bar"
// would be the same path — and only removes the outdated folder under
// LocalName from destDir once that replacement content is fully staged,
// so a failure at any earlier step leaves whatever was already at
// LocalName completely untouched.
func (c *SyncCommand) stageOne(spec SyncSpec) SyncOutcome {
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
		return SyncOutcome{Metadata: Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, Changed: false}
	}

	content, err := c.client.GetSkillContent(spec.Id)
	if err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to retrieve contents for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	rendered, err := renderSkillMarkdown(content, spec.RemoteName)
	if err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to render SKILL.md for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = os.Mkdir(filepath.Join(c.tempDir, spec.RemoteName), 0755); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to stage folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = os.WriteFile(filepath.Join(c.tempDir, spec.RemoteName, "SKILL.md"), []byte(rendered), 0644); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())}
	}

	if err = atomicRemoveAll(filepath.Join(c.destDir, spec.LocalName)); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to remove outdated local skill %s, version %d: %s", spec.LocalName, spec.LocalVersion, err.Error())}
	}

	return SyncOutcome{Metadata: Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, Changed: true}
}

// commitStaged moves every folder stageOne staged under c.tempDir into
// destDir under its final RemoteName. Callers must run this only after
// every stageOne call for specs has returned — including every
// atomicRemoveAll of an outdated LocalName — so that no move performed
// here can still be followed by a sibling spec's delete of that same
// destination path. A spec that was a no-op in stageOne has nothing
// staged and is skipped. Moving is a single same-filesystem os.Rename per
// spec (see tempDirPattern), cheap enough that fanning it out
// concurrently would not be worthwhile.
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
// calls at a time, and returns each call's SyncOutcome at the same index
// as its spec. Use joinErrors on the result to aggregate every per-call
// error into one.
func (c *SyncCommand) boundedFanOut(specs []SyncSpec, fn SyncFn) []SyncOutcome {
	outcomes := make([]SyncOutcome, len(specs))

	var wg sync.WaitGroup

	semaphore := make(chan struct{}, concurrency)

	for i, spec := range specs {
		semaphore <- struct{}{}

		wg.Add(1)

		go func(i int, spec SyncSpec) {
			defer wg.Done()

			defer func() { <-semaphore }()

			outcomes[i] = fn(spec)
		}(i, spec)
	}

	wg.Wait()

	return outcomes
}

// deleteMany removes every spec's LocalName folder from destDir,
// concurrently and atomically, and returns a single joined error naming
// every deletion failure.
func (c *SyncCommand) deleteMany(specs []SyncSpec) error {
	return joinErrors(c.boundedFanOut(specs, func(spec SyncSpec) SyncOutcome {
		return SyncOutcome{Err: atomicRemoveAll(filepath.Join(c.destDir, spec.LocalName))}
	}))
}

// printSummary prints, via c.logger, the first-time sync banner (only
// when pluginJSONCreated is true, since a brand-new
// .claude-plugin/plugin.json is the one condition Claude Code's docs
// confirm needs a new session, decoupled from whether c.destDir itself
// happened to already exist) followed by the sync summary table.
func (c *SyncCommand) printSummary(pluginJSONCreated bool, toCreate []SyncSpec, createOutcomes []SyncOutcome, toUpdate []SyncSpec, updateOutcomes []SyncOutcome, toDelete []SyncSpec) {
	if pluginJSONCreated {
		c.logger.Printf("First time skill sync. The %s plugin is created.", c.pluginRoot)
		c.logger.Println()
	}

	rows := c.buildSummaryRows(toCreate, createOutcomes, toUpdate, updateOutcomes, toDelete)

	var buf bytes.Buffer

	table := tablewriter.NewTable(&buf, tablewriter.WithRenderer(renderer.NewMarkdown()), tablewriter.WithHeaderAutoFormat(tw.Off))

	table.Header([]string{"Skill", "Status", "Previous Version", "Current Version", "Notes"})

	_ = table.Bulk(rows)

	_ = table.Render()

	c.logger.Print(buf.String())
}

// buildSummaryRows builds the sync summary table's rows: one per spec in
// toCreate, toUpdate, and toDelete, grouped by status in the order
// Created, Updated, Unchanged, Removed, Failed, and sorted alphabetically
// by skill name within each group.
func (c *SyncCommand) buildSummaryRows(toCreate []SyncSpec, createOutcomes []SyncOutcome, toUpdate []SyncSpec, updateOutcomes []SyncOutcome, toDelete []SyncSpec) [][]string {
	var created, updated, unchanged, removed, failed []summaryRow

	for i, spec := range toCreate {
		if outcome := createOutcomes[i]; outcome.Err == nil {
			created = append(created, summaryRow{Skill: spec.RemoteName, Status: statusCreated, Previous: "-", Current: strconv.Itoa(spec.RemoteVersion)})
		} else {
			failed = append(failed, summaryRow{Skill: spec.RemoteName, Status: statusFailed, Previous: "-", Current: "-", Notes: outcome.Err.Error()})
		}
	}

	for i, spec := range toUpdate {
		outcome := updateOutcomes[i]

		if outcome.Err != nil {
			failed = append(failed, summaryRow{Skill: spec.LocalName, Status: statusFailed, Previous: strconv.Itoa(spec.LocalVersion), Current: c.currentVersionOnFailure(spec), Notes: outcome.Err.Error()})

			continue
		}

		if outcome.Changed {
			updated = append(updated, summaryRow{Skill: spec.RemoteName, Status: statusUpdated, Previous: strconv.Itoa(spec.LocalVersion), Current: strconv.Itoa(spec.RemoteVersion), Notes: updateNotes(spec)})
		} else {
			unchanged = append(unchanged, summaryRow{Skill: spec.RemoteName, Status: statusUnchanged, Previous: strconv.Itoa(spec.LocalVersion), Current: strconv.Itoa(spec.RemoteVersion)})
		}
	}

	for _, spec := range toDelete {
		removed = append(removed, summaryRow{Skill: spec.LocalName, Status: statusRemoved, Previous: strconv.Itoa(spec.LocalVersion), Current: "-"})
	}

	groups := [][]summaryRow{created, updated, unchanged, removed, failed}

	rows := make([][]string, 0, len(toCreate)+len(toUpdate)+len(toDelete))

	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].Skill < group[j].Skill })

		for _, r := range group {
			rows = append(rows, []string{r.Skill, r.Status, r.Previous, r.Current, escapePipe(r.Notes)})
		}
	}

	return rows
}

// escapePipe backslash-escapes every "|" in s. Unlike a skill name (see
// isSafeSkillName), Notes can carry arbitrary text this package does not
// control — an underlying error's message, which may itself embed a raw
// Registry HTTP response body (see Client.checkStatusCode) — so it must be
// escaped for the summary table's Markdown renderer, which does not escape
// cell content on its own, rather than rejected outright.
func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// updateNotes computes the Notes value for an Updated summary row: a
// rename note takes priority whenever the skill's local folder name
// differs from its remote name; otherwise, if the recorded versions
// already agreed, the update happened only because stageOne found the
// local folder missing, unreadable, or not a directory and recreated it.
func updateNotes(spec SyncSpec) string {
	if spec.LocalName != spec.RemoteName {
		return fmt.Sprintf("renamed from %s", spec.LocalName)
	}

	if spec.LocalVersion == spec.RemoteVersion {
		return recreatedNote
	}

	return ""
}

// currentVersionOnFailure reports the Current Version value for a Failed
// summary row describing a failed update: "-" when destDir/LocalName is
// confirmed absent, or the last recorded LocalVersion otherwise. This is
// safe and accurate only because stageOne (see its doc comment) never
// removes LocalName on any path that can fail, so this report-time stat
// reflects exactly what was on disk before this run started.
func (c *SyncCommand) currentVersionOnFailure(spec SyncSpec) string {
	if _, err := os.Stat(filepath.Join(c.destDir, spec.LocalName)); errors.Is(err, fs.ErrNotExist) {
		return "-"
	}

	return strconv.Itoa(spec.LocalVersion)
}
