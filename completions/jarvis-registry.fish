complete -c jarvis-registry -f

# Root command: subcommands and global flags.
complete -c jarvis-registry -n '__fish_use_subcommand' -a auth -d 'Manage Registry authentication.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a configure -d 'Interactively configure the CLI, e.g. the Registry base URL.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a skills -d 'Manage local skills sync.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s v -l version -d 'Print version and exit.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s h -l help -d 'Show context-sensitive help.'

# configure subcommand: help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from configure' -s h -l help -d 'Show context-sensitive help.'

# auth subcommand: login/status and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status' -a login -d 'Log in to the Registry.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status' -a status -d 'Show Registry authentication status.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth' -s h -l help -d 'Show context-sensitive help.'

# skills subcommand: sync/show and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and not __fish_seen_subcommand_from sync show' -a sync -d 'Sync skills against Jarvis Registry service.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and not __fish_seen_subcommand_from sync show' -a show -d 'Show local skills sync settings.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills' -s h -l help -d 'Show context-sensitive help.'

# skills sync subcommand: project directory, mode, and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and __fish_seen_subcommand_from sync' -l mode -d 'Skills sync mode.' -x -a 'claude codex copilot'
complete -c jarvis-registry -n '__fish_seen_subcommand_from skills; and __fish_seen_subcommand_from sync' -a '(__fish_complete_directories)' -d 'Project directory'

# vim: set ft=fish :
