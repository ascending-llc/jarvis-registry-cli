package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/ascending-llc/jarvis-registry-cli/auth"
	"github.com/ascending-llc/jarvis-registry-cli/cfg"
	"github.com/ascending-llc/jarvis-registry-cli/logging"
)

type (
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
		baseUrl        string
		logger         logging.Logger
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
	// spec and returns its resulting metadata.
	SyncFn func(SyncSpec) (Metadata, error)
)

const (
	skillsReadScope = "skills-read"

	registryBasePath = "/gateway"

	registryDirName = ".jarvis-registry"

	concurrency = 5
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
// before the sync manifest is rewritten to reflect the new state.
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

	// compare local and remote and categorize skills
	toDelete, toUpdate, toCreate := c.getSyncSpecs(localSkills, remoteSkills)

	// MUST do delete->update->create sequentially. This is to avoid a remote skill's name colliding with a local skill's outdated name,
	// because skill name is bound to a local folder name. Once toDelete are deleted and toUpdate get folder names updated to
	// the remote skill names, the server guarantees that there is no name conflict among remote skills returned by ListSkills.
	if err = c.deleteMany(toDelete); err != nil {
		return fmt.Errorf("failed to delete certain skills that user no longer has access to: %s", err.Error())
	}

	updatedSkills, err := c.createOrUpdateMany(toUpdate, c.updateOne)
	if err != nil {
		return fmt.Errorf("failed to update certain skills: %s", err.Error())
	}

	createdSkills, err := c.createOrUpdateMany(toCreate, c.createOne)
	if err != nil {
		return fmt.Errorf("failed to create certain skills: %s", err.Error())
	}

	// write the new manifest file
	if err = c.mrw.WriteManifest(slices.Concat(updatedSkills, createdSkills)); err != nil {
		return fmt.Errorf("failed to write manifest file after syncing: %s", err.Error())
	}

	return nil
}

func (c *SyncCommand) guaranteeDestDir() error {
	if stat, err := os.Stat(c.destDir); errors.Is(err, fs.ErrNotExist) {
		if err = os.Mkdir(c.destDir, 0755); err != nil {
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

	if err = os.Mkdir(filepath.Join(c.destDir, spec.RemoteName), 0755); err != nil {
		return Metadata{}, fmt.Errorf("failed to create folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.WriteFile(filepath.Join(c.destDir, spec.RemoteName, "SKILL.md"), []byte(content.Body), 0644); err != nil {
		return Metadata{}, fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	return Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, nil
}

// updateOne brings the local copy of a skill that exists both locally and
// remotely up to date. It re-fetches and rewrites the skill's folder
// whenever the local name or version differs from the remote, or when the
// expected local folder is missing, unreadable, or not a directory —
// treating any such stat anomaly as needing a refresh. Otherwise it is a
// no-op.
func (c *SyncCommand) updateOne(spec SyncSpec) (Metadata, error) {
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

	if err = os.RemoveAll(filepath.Join(c.destDir, spec.LocalName)); err != nil {
		return Metadata{}, fmt.Errorf("failed to remove outdated local skill %s, version %d: %s", spec.LocalName, spec.LocalVersion, err.Error())
	}

	if err = os.Mkdir(filepath.Join(c.destDir, spec.RemoteName), 0755); err != nil {
		return Metadata{}, fmt.Errorf("failed to create folder for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	if err = os.WriteFile(filepath.Join(c.destDir, spec.RemoteName, "SKILL.md"), []byte(content.Body), 0644); err != nil {
		return Metadata{}, fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", spec.RemoteName, spec.RemoteVersion, err.Error())
	}

	return Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, nil
}

func (c *SyncCommand) createOrUpdateMany(specs []SyncSpec, fn SyncFn) ([]Metadata, error) {
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
