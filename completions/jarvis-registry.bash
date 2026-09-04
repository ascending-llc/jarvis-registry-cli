_jarvis_registry_complete() {
    local cur
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "-v --version -h --help auth sync-skills" -- "$cur") )
        return 0
    fi

    case "${COMP_WORDS[1]}" in
        auth)
            if [ "$COMP_CWORD" -eq 2 ]; then
                COMPREPLY=( $(compgen -W "login status -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            fi
            ;;
        sync-skills)
            case "$cur" in
                -*)
                    COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
                    ;;
                *)
                    COMPREPLY=( $(compgen -d -- "$cur") )
                    ;;
            esac
            ;;
    esac

    return 0
}

complete -F _jarvis_registry_complete jarvis-registry

# vim: set ft=sh :
