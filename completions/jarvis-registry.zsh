#compdef jarvis-registry jr

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

_jarvis-registry_configure() {
    _arguments \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]'
}

_jarvis-registry_skills_sync() {
    _arguments \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]' \
        '--mode[Skills sync mode.]:mode:(claude codex copilot)' \
        '1:project directory:_files -/'
}

_jarvis-registry_skills_show() {
    _arguments \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]'
}

_jarvis-registry_skills() {
    local -a commands
    commands=(
        'sync:Sync skills against Jarvis Registry service.'
        'show:Show local skills sync settings.'
    )
    _arguments -C \
        '(-h --help)'{-h,--help}'[Show context-sensitive help.]' \
        '1: :->command' \
        '*:: :->args' \
        && return 0

    case $state in
        command) _describe 'command' commands ;;
        args)
            case $words[1] in
                sync) _jarvis-registry_skills_sync ;;
                show) _jarvis-registry_skills_show ;;
            esac
            ;;
    esac
}

_jarvis-registry() {
    local -a commands
    commands=(
        'auth:Manage Registry authentication.'
        'configure:Interactively configure the CLI, e.g. the Registry base URL.'
        'skills:Manage local skills sync.'
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
                configure) _jarvis-registry_configure ;;
                skills) _jarvis-registry_skills ;;
            esac
            ;;
    esac
}

_jarvis-registry "$@"

# vim: set ft=zsh :
