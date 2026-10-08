Register-ArgumentCompleter -Native -CommandName 'jarvis-registry', 'jarvis-registry.exe' -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    # Only complete using arguments before the cursor, excluding the current word.
    $words = @(
        foreach ($element in $commandAst.CommandElements) {
            if ($element.Extent.EndOffset -lt $cursorPosition) {
                $element.Extent.Text
            } elseif ($element.Extent.StartOffset -lt $cursorPosition) {
                $wordToComplete = $element.Extent.Text.Substring(0, $cursorPosition - $element.Extent.StartOffset)
            }
        }
    )
    $candidates = '-h', '--help'

    if ($words.Count -le 1) {
        $candidates += 'auth', 'configure', 'skills', 'update', 'completion', '-v', '--version'
    } else {
        switch ($words[1]) {
            'auth' {
                if ($words.Count -eq 2) {
                    $candidates += 'login', 'status', 'logout'
                }
            }
            'update' {
                $candidates += '--check'
            }
            'skills' {
                if ($words.Count -eq 2) {
                    $candidates += 'sync', 'show'
                } elseif ($words[2] -eq 'sync') {
                    if ($words -contains '--') {
                        return
                    }
                    if ($words[-1] -eq '--mode') {
                        $candidates = 'claude', 'codex', 'copilot'
                    } elseif ($wordToComplete -like '-*') {
                        $candidates += '--mode', '-i', '--interactive'
                    } else {
                        # Let PowerShell complete the positional project directory.
                        return
                    }
                }
            }
            'completion' {
                if ($words.Count -eq 2) {
                    $candidates += 'bash', 'zsh', 'fish', 'powershell'
                }
            }
        }
    }

    $candidates | Where-Object { $_ -like "$wordToComplete*" }
}

# vim: set ft=ps1 :
