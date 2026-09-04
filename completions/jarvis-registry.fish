complete -c jarvis-registry -f

# Root command: subcommands and global flags.
complete -c jarvis-registry -n '__fish_use_subcommand' -a auth -d 'Manage Registry authentication.'
complete -c jarvis-registry -n '__fish_use_subcommand' -a sync-skills -d 'Sync skills against Jarvis Registry service.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s v -l version -d 'Print version and exit.'
complete -c jarvis-registry -n '__fish_use_subcommand' -s h -l help -d 'Show context-sensitive help.'

# auth subcommand: login/status and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status' -a login -d 'Log in to the Registry.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth; and not __fish_seen_subcommand_from login status' -a status -d 'Show Registry authentication status.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from auth' -s h -l help -d 'Show context-sensitive help.'

# sync-skills subcommand: project directory and help.
complete -c jarvis-registry -n '__fish_seen_subcommand_from sync-skills' -s h -l help -d 'Show context-sensitive help.'
complete -c jarvis-registry -n '__fish_seen_subcommand_from sync-skills' -a '(__fish_complete_directories)' -d 'Project directory'

# vim: set ft=fish :
