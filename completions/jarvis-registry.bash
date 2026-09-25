_jarvis_registry_complete() {
    local cur
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"

    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "-v --version -h --help auth configure skills update" -- "$cur") )
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
        configure)
            COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            ;;
        update)
            COMPREPLY=( $(compgen -W "-h --help --check" -- "$cur") )
            ;;
        skills)
            case "${COMP_WORDS[2]}" in
                sync)
                    case "$cur" in
                        -*)
                            COMPREPLY=( $(compgen -W "-h --help --mode -i --interactive" -- "$cur") )
                            ;;
                        *)
                            if [ "${COMP_WORDS[COMP_CWORD-1]}" = "--mode" ]; then
                                COMPREPLY=( $(compgen -W "claude codex copilot" -- "$cur") )
                            else
                                COMPREPLY=( $(compgen -d -- "$cur") )
                            fi
                            ;;
                    esac
                    ;;
                show)
                    COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
                    ;;
                *)
                    if [ "$COMP_CWORD" -eq 2 ]; then
                        COMPREPLY=( $(compgen -W "sync show -h --help" -- "$cur") )
                    fi
                    ;;
            esac
            ;;
    esac

    return 0
}

complete -F _jarvis_registry_complete jarvis-registry jr

# vim: set ft=sh :
