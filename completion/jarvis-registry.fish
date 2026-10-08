complete -c jarvis-registry -f

# Root command: subcommands and global flags.
complete -c jarvis-registry -n '__fish_use_subcommand' -a auth -d 'Manage Registry authentication.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a completion -d 'Print a shell completion script to stdout.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a configure -d 'Interactively configure the CLI, e.g. the Registry base URL.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a skills -d 'Manage local skills sync.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a update -d 'Download and install the latest release.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s v -l version -d 'Print version and exit.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s h -l help -d 'Show context-sensitive help.'

# completion subcommand: shell and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish powershell' -a 'bash zsh fish powershell'
complete -c jarvis-registry -n '__fish_seen_subcommand_from completion' -s h -l help -d 'Show context-sensitive help.'

# update subcommand: check and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from update' -l check -d 'Report whether a newer version is available, without installing it.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from update' -s h -l help -d 'Show context-sensitive help.'

# configure subcommand: help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from configure' -s h -l help -d 'Show context-sensitive help.'

# auth subcommand: login/status/logout and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status logout' -a login -d 'Log in to the Registry.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status logout' -a status -d 'Show Registry authentication status.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status logout' -a logout -d 'Log out of the Registry.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth' -s h -l help -d 'Show context-sensitive help.'

# skills subcommand: sync/show and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and not __fish_seen_subcommand_from sync show' -a sync -d 'Sync skills against Jarvis Registry service.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and not __fish_seen_subcommand_from sync show' -a show -d 'Show local skills sync settings.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills' -s h -l help -d 'Show context-sensitive help.'

# skills sync subcommand: project directory, mode, interactive linking, and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and __fish_seen_subcommand_from sync' -s i -l interactive -d 'Prompt before replacing personal-scope skill link collisions.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and __fish_seen_subcommand_from sync' -l mode -d 'Skills sync mode.' -x -a 'claude codex copilot'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and __fish_seen_subcommand_from sync' -a '(__fish_complete_directories)' -d 'Project directory'

# vim: set ft=fish :
