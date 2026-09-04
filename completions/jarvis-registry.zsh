#compdef jarvis-registry

_jarvis-registry_auth() {
    local -a commands
    commands=(
        'login:Log in to the Registry.'
        'status:Show Registry authentication status.'
    )
    _arguments -C \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]' \
        '1: :->command' \
        && return 0

    if [[ $state == command ]]; then
        _describe 'command' commands
    fi
}

_jarvis-registry_sync-skills() {
    _arguments \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]' \
        '1:project directory:_files -/'
}

_jarvis-registry() {
    local -a commands
    commands=(
        'auth:Manage Registry authentication.'
        'sync-skills:Sync skills against Jarvis Registry service.'
    )

    _arguments -C \
        '(-v --version)'{-v,--version}'[Print version and exit.]' \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]' \
        '1: :->command' \
        '*:: :->args' \
        && return 0

    case $state in
        command)
            _describe 'command' commands
            ;;
        args)
            case $words[1] in
                auth) _jarvis-registry_auth ;;
                sync-skills) _jarvis-registry_sync-skills ;;
            esac
            ;;
    esac
}

_jarvis-registry "$@"

# vim: set ft=zsh :
