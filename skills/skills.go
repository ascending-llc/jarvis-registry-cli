package skills

import (
	"bytes"
	"encoding/base64"
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

	// SyncCommand implements the "skills sync" subcommand: it reconciles
	// the local skills folder against the skills available to the
	// caller on the Registry, creating, updating, and deleting local
	// skill folders as needed.
	SyncCommand struct { //nolint:govet // fieldalignment: keep the exported CLI arguments first and related configuration fields together.
		// ProjectPath is the directory skills are synced under. Omit it for
		// personal scope: claude writes into ~/.claude/skills/jarvis-registry/;
		// codex/copilot write into ~/.jarvis-registry/skills/<mode>/ and link
		// into ~/.codex/skills/ or ~/.copilot/skills/. With an explicit path,
		// claude uses <path>/.claude/skills/jarvis-registry/, codex
		// <path>/.agents/skills/, and copilot <path>/.github/skills/. Relative
		// paths (including "." for the current directory) resolve against the
		// current working directory. Codex/copilot still refuse an explicit
		// home-directory path; omit the argument instead.
		ProjectPath string `arg:"" optional:"" help:"Project directory to sync under (relative paths, including \".\", resolve against the current working directory). Omit for personal scope: claude uses ~/.claude/skills/jarvis-registry/; codex/copilot use ~/.jarvis-registry/skills/<mode>/ with links under ~/.codex/skills/ or ~/.copilot/skills/. With a path, codex uses <path>/.agents/skills/ and copilot <path>/.github/skills/."`

		// Mode selects the skills-directory convention to target. It
		// overrides local.skills.mode from config; one of the two must
		// resolve to a value.
		Mode string `optional:"" help:"Skills sync mode: claude, codex, or copilot. Overrides local.skills.mode from config; one of the two must resolve to a value."`

		// Interactive prompts before replacing a colliding entry when
		// reconciling personal-scope Codex/Copilot skill links. It has no
		// effect in claude mode or when a project path is given.
		Interactive bool `short:"i" help:"For personal-scope codex/copilot sync, prompt before replacing an existing file/folder/symlink that collides with a skill's symlink. No effect for claude mode or when a project path is given. Defaults to off."`

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
		skipIds        []string
		tempDir        string
		userHomeDir    string
		mrw            ManifestReadWriter
		syncRoot       string
		registryDir    string
		mode           cfg.SkillsMode
		personalScope  bool
		override       bool
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

	// skippedSkill is a remote skill Run will not sync, together with why.
	skippedSkill struct {
		Reason string
		Metadata
	}

	// summaryRow is one row of the sync summary table Run prints when it
	// finishes. Link is populated only for personal-scope Codex/Copilot
	// runs.
	summaryRow struct {
		Skill    string
		Status   string
		Previous string
		Current  string
		Notes    string
		Link     string
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
	statusSkipped   = "Skipped"

	// chatOwnedFilesSkippedNote is the Notes value for a Skipped summary
	// row: a multi-file skill created in Jarvis Chat, whose supporting
	// files are pointers into Chat's external storage adapter that Jarvis
	// Registry cannot read, so the CLI cannot sync it.
	chatOwnedFilesSkippedNote = "not synced: supporting files were created in Jarvis Chat and are not readable from Jarvis Registry"

	// userSkippedNote is the Notes value for a Skipped summary row describing
	// a skill the caller explicitly excluded in the local CLI configuration.
	userSkippedNote = "skipped: configured in local.skills.skip_ids"

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

// AfterApply derives SyncCommand's remaining dependencies. Config is
// loaded first because the sync destination now depends on the resolved
// mode (flag over config) and whether a project path was supplied.
func (c *SyncCommand) AfterApply() (err error) {
	c.registryDir = filepath.Join(c.userHomeDir, cfg.RegistryDirName)

	config, err := c.configLoadFunc(c.registryDir)
	if err != nil {
		return fmt.Errorf("failed to load config options: %s", err.Error())
	}

	if c.mode, err = resolveSkillsMode(c.Mode, config.Local.Skills.Mode); err != nil {
		return err
	}

	c.personalScope = c.mode != cfg.SkillsModeClaude && strings.TrimSpace(c.ProjectPath) == ""
	if c.personalScope {
		c.syncRoot = personalScopeSyncRoot(c.registryDir, c.mode)
		c.destDir = c.syncRoot
	} else {
		resolvedProjectPath, resolveErr := resolveProjectPath(c.ProjectPath)
		if resolveErr != nil {
			return fmt.Errorf("invalid project path %q: %s", c.ProjectPath, resolveErr.Error())
		}

		if c.mode != cfg.SkillsModeClaude && isHomeDir(resolvedProjectPath, c.userHomeDir) {
			return fmt.Errorf("%s mode does not support syncing into the user's home directory as a project; omit the path for personal scope", c.mode)
		}

		c.syncRoot, c.destDir = destinationsForMode(c.mode, resolvedProjectPath)
	}

	if c.personalScope && c.Interactive && !c.isTerminal() {
		return errors.New("-i/--interactive was passed but stdin is not a terminal; drop the flag or run from an interactive terminal")
	}

	c.baseUrl = config.Registry.BaseUrl

	c.authBaseUrl = config.Registry.AuthBaseUrl

	c.skipIds = config.Local.Skills.SkipIds
	c.override = config.Local.Skills.Link.Override

	c.tp = auth.NewRegistryTokenResolver(c.authBaseUrl, auth.RegistryScopes, c.logger)

	return nil
}

// resolveSkillsMode resolves the effective mode: flagValue (the --mode
// flag) wins if non-empty and valid; otherwise configValue (already
// validated by cfg.Load) is used. Neither present is a hard failure — mode
// has no default.
func resolveSkillsMode(flagValue string, configValue cfg.SkillsMode) (cfg.SkillsMode, error) {
	if flagValue != "" {
		mode := cfg.SkillsMode(flagValue)
		if !mode.Valid() {
			return "", fmt.Errorf("invalid --mode %q: must be one of claude, codex, copilot", flagValue)
		}

		return mode, nil
	}

	if configValue != "" {
		return configValue, nil
	}

	return "", errors.New("no sync mode resolved: pass --mode, or set local.skills.mode via `jarvis-registry configure`")
}

// isHomeDir reports whether projectPath is the user's home directory. It
// first compares the two lexically (Clean'd), which also covers a
// projectPath that doesn't exist yet — such a path can't be home. It then
// compares by file identity via os.SameFile (os.Stat follows symlinks), so a
// symlink or other alternate path resolving to home can't slip the
// personal-scope refusal that gates codex/copilot.
func isHomeDir(projectPath, homeDir string) bool {
	if filepath.Clean(projectPath) == filepath.Clean(homeDir) {
		return true
	}

	projectInfo, projectErr := os.Stat(projectPath)
	homeInfo, homeErr := os.Stat(homeDir)

	return projectErr == nil && homeErr == nil && os.SameFile(projectInfo, homeInfo)
}

// destinationsForMode returns the directory that owns skill-lock.json and
// gates consent/locking (syncRoot), and the directory skills are written
// directly into (destDir), for mode under the resolved project path.
// Claude mode keeps its plugin-owned subtree, where the two differ; codex
// and copilot have no such subtree, so syncRoot and destDir are the same
// flat, ecosystem-shared directory.
func destinationsForMode(mode cfg.SkillsMode, projectPath string) (syncRoot, destDir string) {
	switch mode {
	case cfg.SkillsModeCodex:
		d := filepath.Join(projectPath, ".agents", "skills")

		return d, d
	case cfg.SkillsModeCopilot:
		d := filepath.Join(projectPath, ".github", "skills")

		return d, d
	case cfg.SkillsModeClaude:
	}

	// claude (and any unresolved value, though AfterApply guarantees a valid
	// mode before this is called) uses the plugin-owned subtree, where
	// syncRoot and destDir differ by one level.
	root := filepath.Join(projectPath, ".claude", "skills", "jarvis-registry")

	return root, filepath.Join(root, "skills")
}

// Run rejects aliased personal content/link directories, resolves a
// Registry access token, then reconciles the local skills
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
	if c.personalScope {
		if _, err = c.checkUserScopeSkillsDir(); err != nil {
			return err
		}
	}

	// initialize the final two dependencies c.client and c.mrw
	token, err := c.tp.GetAccessToken()
	if err != nil {
		return fmt.Errorf("failed to get Registry access token: %s", err.Error())
	}

	if c.client, err = NewClient(c.baseUrl+registryBasePath, token); err != nil {
		return fmt.Errorf("failed to create Registry client: %s", err.Error())
	}

	c.mrw = NewManifestReadWriter(c.syncRoot)

	// acquire the advisory lock for this sync root before touching the
	// filesystem at all, so two concurrent invocations against the same
	// target can't race on the consent check or the bootstrap writes
	release, err := acquireLock(c.registryDir, c.syncRoot)
	if err != nil {
		return err
	}

	defer release()

	// gate any mutation of a pre-existing, possibly foreign sync root
	if err = c.ensureSyncRootConsent(); err != nil {
		return err
	}

	// consent already granted: create the sync root if it doesn't exist yet
	if err = os.MkdirAll(c.syncRoot, 0755); err != nil {
		return fmt.Errorf("failed to create sync root at %s: %s", c.syncRoot, err.Error())
	}

	// reconcile the CLI-owned plugin manifest (claude mode only — codex and
	// copilot have no plugin.json concept). firstTime is the "was this
	// genuinely the first sync into this destination" signal that drives the
	// summary banner: plugin.json's non-existence for claude, the manifest's
	// own non-existence otherwise. c.mrw.Exists() must be read before
	// WriteManifest runs (at the end of Run), which this placement satisfies.
	var firstTime bool

	if c.mode == cfg.SkillsModeClaude {
		if firstTime, err = reconcilePluginManifest(c.syncRoot, c.stderrLogger); err != nil {
			return err
		}
	} else {
		firstTime = !c.mrw.Exists()
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

	localSkills := make([]Metadata, len(manifest.Skills))
	for i, s := range manifest.Skills {
		localSkills[i] = Metadata{Id: s.Id, Name: s.Name, Version: s.Version}
	}

	// reconcile the CLI-owned sync-skills wrapper skill
	newSyncSkillsVersion, err := reconcileSyncSkillsWrapper(c.destDir, manifest.SyncSkillsVersion, c.mode, c.stderrLogger)
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
	// or that collides with one of this CLI's own reserved entries — its
	// wrapper skill folder, or (for codex/copilot, where the manifest lives
	// directly inside destDir) the skill-lock.json manifest file — before it
	// reaches any os.* call. Both reservations are matched case-insensitively
	// because a Registry name's casing is uncontrolled and the destination
	// filesystem may itself be case-insensitive.
	for _, r := range remoteSkills {
		if !isSafeSkillName(r.Name) {
			return fmt.Errorf("remote skill %s (id %s) has a name that is unsafe to use as a filesystem path or in the sync summary table", r.Name, r.Id)
		}

		if strings.EqualFold(r.Name, reservedSyncSkillsName) {
			return fmt.Errorf("remote skill %s (id %s) is named %q, which is reserved for this CLI's own wrapper skill", r.Name, r.Id, reservedSyncSkillsName)
		}

		if strings.EqualFold(r.Name, manifestFileName) {
			return fmt.Errorf("remote skill %s (id %s) is named %q, which is reserved for this CLI's own sync manifest", r.Name, r.Id, manifestFileName)
		}
	}

	// Set aside remote skills this run must not sync. They are reported only
	// via the summary's Skipped group and never reach the content endpoint.
	eligibleSkills, skippedSkills, unmatchedSkipIds := partitionSkippableSkills(remoteSkills, c.skipIds)

	if len(unmatchedSkipIds) > 0 {
		c.stderrLogger.Printf("warning: local.skills.skip_ids lists %d id(s) that don't match any skill currently available to you (check for a typo, revoked access, or a skill Name pasted where an Id belongs): %s", len(unmatchedSkipIds), strings.Join(unmatchedSkipIds, ", "))
	}

	// compare local and remote and categorize skills
	toDelete, toUpdate, toCreate := c.getSyncSpecs(localSkills, eligibleSkills)

	// MUST do delete->update->create sequentially. This is to avoid a remote skill's name colliding with a local skill's outdated name,
	// because skill name is bound to a local folder name. Once toDelete are deleted and toUpdate get folder names updated to
	// the remote skill names, the server guarantees that there is no name conflict among remote skills returned by ListSkills.
	if err = c.deleteMany(toDelete); err != nil {
		return fmt.Errorf("failed to delete skills no longer in the desired local state: %s", err.Error())
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

	if err = c.commitStaged(updateOutcomes); err != nil {
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

	var (
		linkOutcomes []linkOutcome
		linkErr      error
	)

	if c.personalScope {
		names := make([]string, 0, len(succeeded)+1)

		names = append(names, reservedSyncSkillsName)
		for _, m := range succeeded {
			names = append(names, m.Name)
		}

		// A desired name may point at a different, now-deleted owned skill.
		// Prune first so reconciliation repairs it in this same run. Keep
		// reconciliation last in the summary's per-name outcome map.
		pruned, pruneErr := c.pruneDanglingLinks()
		reconciled, reconcileErr := c.reconcileSymlinks(names)
		linkOutcomes = slices.Concat(pruned, reconciled)
		linkErr = errors.Join(reconcileErr, pruneErr, joinLinkErrors(linkOutcomes))
	}

	c.printSummary(firstTime, toCreate, createOutcomes, toUpdate, updateOutcomes, toDelete, skippedSkills, linkOutcomes)

	return errors.Join(joinErrors(updateOutcomes), joinErrors(createOutcomes), linkErr)
}

// partitionSkippableSkills splits remote into skills this run will sync and
// skills it will not, carrying the reason for each skip. Chat-owned multi-file
// skills take precedence over user-configured skips because removing the
// latter cannot make their supporting files readable from Registry. It also
// returns configured Ids that matched no remote skill. skipIds is expected to
// have already been trimmed and de-duplicated by cfg.Load.
func partitionSkippableSkills(remote []Metadata, skipIds []string) (eligible []Metadata, skipped []skippedSkill, unmatched []string) {
	skipSet := make(map[string]struct{}, len(skipIds))
	for _, id := range skipIds {
		skipSet[id] = struct{}{}
	}

	matched := make(map[string]struct{}, len(skipIds))

	for _, m := range remote {
		_, userSkip := skipSet[m.Id]
		if userSkip {
			matched[m.Id] = struct{}{}
		}

		switch {
		case m.FileCount > 0 && !m.CreatedByRegistry:
			skipped = append(skipped, skippedSkill{Metadata: m, Reason: chatOwnedFilesSkippedNote})
		case userSkip:
			skipped = append(skipped, skippedSkill{Metadata: m, Reason: userSkippedNote})
		default:
			eligible = append(eligible, m)
		}
	}

	for _, id := range skipIds {
		if _, ok := matched[id]; !ok {
			unmatched = append(unmatched, id)
		}
	}

	return eligible, skipped, unmatched
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

// isSafeRelativeFilePath reports whether path is safe to join under a
// skill's staged folder before writing a supporting file to it. Like
// isSafeSkillName, this is defense-in-depth against a misbehaving or
// compromised Registry response: filepath.Join only lexically normalizes
// its arguments and does not confine the result to the staging folder, so
// an absolute path or one that escapes via ".." must be rejected
// explicitly, before any os.MkdirAll/os.WriteFile sees it. A backslash is
// rejected outright so a Windows-style separator can never smuggle path
// segments past the forward-slash-based cleaning below.
func isSafeRelativeFilePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) {
		return false
	}

	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return false
		}
	}

	cleaned := filepath.Clean(path)

	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
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
// never be deleted regardless of manifest content — and manifestFileName,
// which for codex/copilot lives directly inside destDir (a no-op for
// claude, where skill-lock.json lives one level up in syncRoot and this
// scan never sees it). manifestFileName is matched case-sensitively: it is
// a literal constant this CLI always writes with the same casing, unlike a
// Registry-supplied skill name.
func (c *SyncCommand) cleanDestDir(skills []Metadata) error {
	entries, err := os.ReadDir(c.destDir)
	if err != nil {
		return fmt.Errorf("failed to list contents of the skills folder at %s: %s", c.destDir, err.Error())
	}

	toRemove := make(map[string]struct{}, len(entries))

	for _, e := range entries {
		if strings.EqualFold(e.Name(), reservedSyncSkillsName) || e.Name() == manifestFileName {
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

// stageSkillContent validates every file in content.Files (availability
// and relative-path safety) before writing anything, builds folder as a
// brand-new directory, and writes SKILL.md plus every supporting file into
// it. folder must not already exist. On failure, any directory created
// here is removed; commitStaged also excludes failed outcomes so a cleanup
// failure cannot publish partial content. The destination is never touched.
func stageSkillContent(folder, remoteName string, remoteVersion int, content Content) (err error) {
	for _, f := range content.Files {
		if !f.Available {
			return fmt.Errorf("supporting file %q for remote skill %s, version %d is not available: %s", f.RelativePath, remoteName, remoteVersion, f.UnavailableReason)
		}

		if !isSafeRelativeFilePath(f.RelativePath) {
			return fmt.Errorf("supporting file %q for remote skill %s, version %d has an unsafe relative path", f.RelativePath, remoteName, remoteVersion)
		}
	}

	rendered, err := renderSkillMarkdown(content, remoteName)
	if err != nil {
		return fmt.Errorf("failed to render SKILL.md for remote skill %s, version %d: %s", remoteName, remoteVersion, err.Error())
	}

	if err = os.Mkdir(folder, 0755); err != nil {
		return fmt.Errorf("failed to stage folder for remote skill %s, version %d: %s", remoteName, remoteVersion, err.Error())
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(folder)
		}
	}()

	if err = os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(rendered), 0644); err != nil {
		return fmt.Errorf("failed to write SKILL.md file for remote skill %s, version %d: %s", remoteName, remoteVersion, err.Error())
	}

	for _, f := range content.Files {
		target := filepath.Join(folder, filepath.FromSlash(f.RelativePath))

		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to stage supporting file %q for remote skill %s, version %d: %s", f.RelativePath, remoteName, remoteVersion, err.Error())
		}

		mode := os.FileMode(0644)
		if f.IsExecutable {
			mode = 0755
		}

		// Registry sets exactly one of Content and Body on an available
		// file: Content holds the file's text, Body holds base64-encoded
		// bytes whenever the file could not be represented as UTF-8 text
		// (a genuinely binary file, but also a file stored with
		// IsBinary=false whose bytes still fail to decode as UTF-8 — see
		// Registry's _sync_file_response/_registry_file_text). Keying the
		// decode off Body's presence rather than the IsBinary flag alone
		// therefore handles every case Registry emits; IsBinary is
		// informational.
		data := []byte(f.Content)

		if f.Body != "" {
			if data, err = base64.StdEncoding.DecodeString(f.Body); err != nil {
				return fmt.Errorf("failed to decode supporting file %q for remote skill %s, version %d: %s", f.RelativePath, remoteName, remoteVersion, err.Error())
			}
		}

		if err = os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("failed to write supporting file %q for remote skill %s, version %d: %s", f.RelativePath, remoteName, remoteVersion, err.Error())
		}
	}

	return nil
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

	if err = stageSkillContent(filepath.Join(c.tempDir, spec.RemoteName), spec.RemoteName, spec.RemoteVersion, content); err != nil {
		return SyncOutcome{Err: err}
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

	if err = stageSkillContent(filepath.Join(c.tempDir, spec.RemoteName), spec.RemoteName, spec.RemoteVersion, content); err != nil {
		return SyncOutcome{Err: err}
	}

	if err = atomicRemoveAll(filepath.Join(c.destDir, spec.LocalName)); err != nil {
		return SyncOutcome{Err: fmt.Errorf("failed to remove outdated local skill %s, version %d: %s", spec.LocalName, spec.LocalVersion, err.Error())}
	}

	return SyncOutcome{Metadata: Metadata{Id: spec.Id, Name: spec.RemoteName, Version: spec.RemoteVersion}, Changed: true}
}

// commitStaged moves only successfully changed outcomes from c.tempDir
// into destDir. Callers must run this only after
// every stageOne call has returned — including every
// atomicRemoveAll of an outdated LocalName — so that no move performed
// here can still be followed by a sibling spec's delete of that same
// destination path. Failed and unchanged outcomes are skipped even if a
// temporary folder exists. Moving is a single same-filesystem os.Rename per
// outcome (see tempDirPattern), cheap enough that fanning it out
// concurrently would not be worthwhile.
func (c *SyncCommand) commitStaged(outcomes []SyncOutcome) error {
	errs := make([]error, 0, len(outcomes))

	for _, outcome := range outcomes {
		if outcome.Err != nil || !outcome.Changed {
			continue
		}

		staged := filepath.Join(c.tempDir, outcome.Metadata.Name)

		if err := os.Rename(staged, filepath.Join(c.destDir, outcome.Metadata.Name)); err != nil {
			errs = append(errs, fmt.Errorf("failed to move staged skill %s, version %d into place: %s", outcome.Metadata.Name, outcome.Metadata.Version, err.Error()))
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

// printSummary prints, via c.logger, a scope line for Codex/Copilot, the
// first-time sync banner (only when firstTime is true), then the sync
// summary table. For claude mode firstTime tracks a brand-new
// .claude-plugin/plugin.json — the one condition Claude Code's docs
// confirm needs a new session, decoupled from whether c.destDir itself
// happened to already exist; for codex/copilot it tracks the manifest's
// own first appearance in the destination.
func (c *SyncCommand) printSummary(firstTime bool, toCreate []SyncSpec, createOutcomes []SyncOutcome, toUpdate []SyncSpec, updateOutcomes []SyncOutcome, toDelete []SyncSpec, skipped []skippedSkill, links []linkOutcome) {
	if c.mode != cfg.SkillsModeClaude {
		scope := "project"
		if c.personalScope {
			scope = "personal"
		}

		c.logger.Printf("Sync scope: %s (%s)", scope, c.syncRoot)
	}

	if firstTime {
		switch c.mode {
		case cfg.SkillsModeClaude:
			c.logger.Printf("First time skill sync. The %s plugin is created.", c.syncRoot)
		case cfg.SkillsModeCodex, cfg.SkillsModeCopilot:
			c.logger.Printf("First time skill sync into %s.", c.syncRoot)
		}

		c.logger.Println()
	}

	rows := c.buildSummaryRows(toCreate, createOutcomes, toUpdate, updateOutcomes, toDelete, skipped, links)

	var buf bytes.Buffer

	table := tablewriter.NewTable(&buf, tablewriter.WithRenderer(renderer.NewMarkdown()), tablewriter.WithHeaderAutoFormat(tw.Off))

	header := []string{"Skill", "Status", "Previous Version", "Current Version", "Notes"}
	if c.personalScope {
		header = append(header, "Link")
	}

	table.Header(header)

	_ = table.Bulk(rows)

	_ = table.Render()

	c.logger.Print(buf.String())
}

// buildSummaryRows builds the sync summary table's rows: one per spec in
// toCreate, toUpdate, and toDelete plus one per skipped skill, grouped by
// status in the order Created, Updated, Unchanged, Removed, Failed,
// Skipped, and sorted alphabetically by skill name within each group.
func (c *SyncCommand) buildSummaryRows(toCreate []SyncSpec, createOutcomes []SyncOutcome, toUpdate []SyncSpec, updateOutcomes []SyncOutcome, toDelete []SyncSpec, skipped []skippedSkill, links []linkOutcome) [][]string {
	var created, updated, unchanged, removed, failed, skippedRows []summaryRow

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

	for _, s := range skipped {
		skippedRows = append(skippedRows, summaryRow{Skill: s.Name, Status: statusSkipped, Previous: "-", Current: strconv.Itoa(s.Version), Notes: s.Reason})
	}

	groups := [][]summaryRow{created, updated, unchanged, removed, failed, skippedRows}

	var ordered []summaryRow

	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].Skill < group[j].Skill })

		ordered = append(ordered, group...)
	}

	return c.renderSummaryRows(ordered, links)
}

// renderSummaryRows joins link results onto matching content rows and adds
// rows for the built-in wrapper and dangling-link cleanup without a content row.
func (c *SyncCommand) renderSummaryRows(content []summaryRow, links []linkOutcome) [][]string {
	byName := make(map[string]linkOutcome, len(links))
	for _, link := range links {
		byName[link.Name] = link
	}

	represented := make(map[string]bool, len(content))
	for _, row := range content {
		if link, ok := byName[row.Skill]; ok && linkAppliesToRow(row, link) {
			represented[row.Skill] = true
		}
	}

	var extra []summaryRow
	if c.personalScope {
		for name := range byName {
			if !represented[name] {
				note := "dangling link cleanup"
				if name == reservedSyncSkillsName {
					note = "built-in sync-skills wrapper"
				}

				extra = append(extra, summaryRow{Skill: name, Status: "-", Previous: "-", Current: "-", Notes: note})
			}
		}
	}

	sort.Slice(extra, func(i, j int) bool { return extra[i].Skill < extra[j].Skill })

	rows := make([][]string, 0, len(content)+len(extra))
	for _, row := range slices.Concat(content, extra) {
		row.Link = "-"
		if link, ok := byName[row.Skill]; c.personalScope && ok && linkAppliesToRow(row, link) {
			row.Link = link.Status
			if link.Err != nil {
				row.Notes = strings.TrimPrefix(row.Notes+"; "+link.Err.Error(), "; ")
			}
		}

		cells := []string{row.Skill, row.Status, row.Previous, row.Current, escapePipe(row.Notes)}
		if c.personalScope {
			cells = append(cells, row.Link)
		}

		rows = append(rows, cells)
	}

	return rows
}

func linkAppliesToRow(row summaryRow, link linkOutcome) bool {
	switch row.Status {
	case statusCreated, statusUpdated, statusUnchanged:
		return link.Status != linkStatusRemoved
	case statusRemoved:
		return link.Status == linkStatusRemoved
	case "-":
		return true
	default:
		return false
	}
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
