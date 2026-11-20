package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// dangerousShellTokens rejects compound commands, pipes, redirections, and command substitutions
// from the strict read-only allowlist.
var dangerousShellTokens = []string{
	"|", ">", "<", ";", "&", "\n", "\r", "$(", "`",
}

// powerShellWriteCmdlets are explicitly denied from the read-only allowlist on all platforms.
var powerShellWriteCmdlets = map[string]bool{
	"out-file":          true,
	"set-content":       true,
	"add-content":       true,
	"clear-content":     true,
	"remove-item":       true,
	"move-item":         true,
	"copy-item":         true,
	"new-item":          true,
	"rename-item":       true,
	"invoke-expression": true,
	"iex":               true,
	"start-process":     true,
	"stop-process":      true,
	"kill":              true,
	"rm":                true,
	"del":               true,
	"rmdir":             true,
	"mv":                true,
	"cp":                true,
}

// highRiskCommands require explicit one-time confirmation even in full-access mode.
var highRiskCommands = map[string]bool{
	"rm":       true,
	"rmdir":    true,
	"sudo":     true,
	"su":       true,
	"mkfs":     true,
	"dd":       true,
	"chmod":    true,
	"chown":    true,
	"shutdown": true,
	"reboot":   true,
	"halt":     true,
	"kill":     true,
	"killall":  true,
	"pkill":    true,
	"diskutil": true,
	"format":   true,
}

func classifyFileToolCall(req ToolExecuteRequest, perm string) ToolClassifyResult {
	if perm == PermissionReadOnly && isWriteFileTool(req.ToolName) {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("tool %q is not allowed in read-only mode", req.ToolName),
		}
	}

	root, _, err := resolveRootForRequest(req, false)
	if err != nil {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   err.Error(),
		}
	}

	rawPath := extractTargetPath(req)
	if rawPath == "" {
		if req.ToolName == "glob" || req.ToolName == "grep" {
			if root == "" {
				return ToolClassifyResult{
					Decision: DecisionDeny,
					Reason:   fmt.Sprintf("tool %q requires an active workspace or explicit path", req.ToolName),
				}
			}
			return ToolClassifyResult{Decision: DecisionAllow}
		}
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	followLeaf := req.ToolName != "delete_file"
	resolved, withinRoot, err := resolveAndValidatePath(rawPath, root, true, followLeaf)
	if err != nil {
		if errors.Is(err, errBrokenSymlink) {
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   err.Error(),
			}
		}
		if root == "" && perm != PermissionFullAccess {
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   err.Error(),
			}
		}
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	// Never allow deleting filesystem root or workspace root.
	if req.ToolName == "delete_file" {
		if resolved == filepath.Dir(resolved) {
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   "refusing to delete filesystem root",
			}
		}
		if root != "" {
			if canonicalRoot, cErr := canonicalizePath(root); cErr == nil && resolved == canonicalRoot {
				return ToolClassifyResult{
					Decision: DecisionDeny,
					Reason:   "refusing to delete workspace root directory",
				}
			}
		}
	}

	if !withinRoot {
		switch perm {
		case PermissionReadOnly:
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   fmt.Sprintf("path %q is outside the allowed workspace", rawPath),
			}
		case PermissionWorkspaceWrite:
			return ToolClassifyResult{
				Decision: DecisionAsk,
				Reason:   fmt.Sprintf("path %q is outside the current workspace and requires one-time approval", rawPath),
			}
		case PermissionFullAccess:
			if req.ToolName == "delete_file" {
				return ToolClassifyResult{
					Decision: DecisionAsk,
					Reason:   fmt.Sprintf("deleting %q outside the workspace requires one-time confirmation", rawPath),
				}
			}
			return ToolClassifyResult{Decision: DecisionAllow}
		}
	}

	return ToolClassifyResult{Decision: DecisionAllow}
}

func classifyBashToolCall(req ToolExecuteRequest, perm string) ToolClassifyResult {
	cmdStr := strings.TrimSpace(stringInput(req.Input, "command"))
	if cmdStr == "" {
		// Let ExecuteTool return a clean parameter error.
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	// Validate explicit workdir override if supplied.
	inputWorkdir := strings.TrimSpace(stringInput(req.Input, "workdir"))
	if inputWorkdir != "" && strings.TrimSpace(req.Workdir) != "" {
		_, withinRoot, err := resolveAndValidatePath(inputWorkdir, req.Workdir, true, true)
		if err != nil {
			if errors.Is(err, errBrokenSymlink) {
				return ToolClassifyResult{Decision: DecisionDeny, Reason: err.Error()}
			}
		} else if !withinRoot {
			if perm == PermissionReadOnly {
				return ToolClassifyResult{
					Decision: DecisionDeny,
					Reason:   fmt.Sprintf("workdir %q is outside the allowed workspace", inputWorkdir),
				}
			}
			if perm == PermissionWorkspaceWrite {
				return ToolClassifyResult{
					Decision: DecisionAsk,
					Reason:   fmt.Sprintf("running command in %q outside the workspace requires one-time approval", inputWorkdir),
				}
			}
		}
	} else if inputWorkdir != "" && strings.TrimSpace(req.Workdir) == "" && perm == PermissionReadOnly {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   "explicit workdir is not allowed in read-only mode without an active workspace",
		}
	}

	if isStrictReadOnlyCommand(cmdStr) {
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	switch perm {
	case PermissionReadOnly:
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   "command is not in the strict read-only allowlist",
		}
	case PermissionWorkspaceWrite:
		return ToolClassifyResult{
			Decision: DecisionAsk,
			Reason:   "non-read-only command requires one-time approval",
		}
	case PermissionFullAccess:
		if isHighRiskCommand(cmdStr) {
			return ToolClassifyResult{
				Decision: DecisionAsk,
				Reason:   "high-risk command requires one-time confirmation",
			}
		}
		return ToolClassifyResult{Decision: DecisionAllow}
	default:
		return ToolClassifyResult{
			Decision: DecisionAsk,
			Reason:   "command requires one-time approval",
		}
	}
}

func isHighRiskCommand(cmdStr string) bool {
	lower := strings.ToLower(cmdStr)
	for _, tok := range []string{"rm -rf", "rm -fr", "sudo ", "mkfs", "dd if=", "shutdown", "reboot", "remove-item"} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	words, ok := splitShellWords(cmdStr)
	if !ok || len(words) == 0 {
		return true
	}
	cmd := strings.ToLower(filepath.Base(words[0]))
	return highRiskCommands[cmd]
}

// isStrictReadOnlyCommand returns true ONLY for single, non-compound commands whose
// subcommand and every flag are explicitly allowlisted as side-effect-free read-only operations.
func isStrictReadOnlyCommand(cmdStr string) bool {
	for _, tok := range dangerousShellTokens {
		if strings.Contains(cmdStr, tok) {
			return false
		}
	}

	words, ok := splitShellWords(cmdStr)
	if !ok || len(words) == 0 {
		return false
	}

	// Reject inline env assignments (e.g. FOO=bar cmd) from automatic allowlist.
	if strings.Contains(words[0], "=") {
		return false
	}

	cmd := strings.ToLower(filepath.Base(words[0]))
	if powerShellWriteCmdlets[cmd] {
		return false
	}
	args := words[1:]

	switch cmd {
	case "pwd":
		return allFlagsAllowed(args, map[string]bool{"-L": true, "-P": true}, nil)
	case "whoami", "hostname", "arch", "date", "id":
		return len(args) == 0
	case "uname":
		return allFlagsAllowed(args, map[string]bool{
			"-a": true, "-s": true, "-n": true, "-r": true, "-v": true, "-m": true, "-p": true,
		}, nil)
	case "which":
		return allFlagsAllowed(args, map[string]bool{"-a": true}, nil)
	case "echo":
		return allFlagsAllowed(args, map[string]bool{"-n": true}, nil)
	case "ls":
		return isAllowedLsArgs(args)
	case "cat":
		return allFlagsAllowed(args, map[string]bool{"-n": true, "-b": true, "-s": true}, nil)
	case "head":
		return allFlagsAllowed(args, nil, map[string]bool{"-n": true, "-c": true})
	case "tail":
		// Explicitly exclude -f / -F (follow) so read-only commands never block indefinitely.
		return allFlagsAllowed(args, nil, map[string]bool{"-n": true, "-c": true})
	case "wc":
		return allFlagsAllowed(args, map[string]bool{"-l": true, "-w": true, "-c": true, "-m": true}, nil)
	case "file":
		return allFlagsAllowed(args, map[string]bool{"-b": true, "-i": true, "--mime-type": true, "--brief": true}, nil)
	case "stat":
		return allFlagsAllowed(args, map[string]bool{"-x": true}, nil)
	case "realpath", "dirname", "basename":
		return allFlagsAllowed(args, nil, nil)
	case "grep":
		return allFlagsAllowed(args, map[string]bool{
			"-n": true, "-i": true, "-r": true, "-R": true, "-l": true, "-c": true,
			"-E": true, "-F": true, "-H": true, "-h": true, "-v": true, "-w": true,
			"-o": true, "-q": true, "-s": true, "--line-number": true, "--ignore-case": true,
			"--files-with-matches": true, "--count": true, "--recursive": true,
		}, map[string]bool{
			"-A": true, "-B": true, "-C": true, "-m": true, "-e": true, "--include": true, "--exclude": true,
		})
	case "rg":
		return allFlagsAllowed(args, map[string]bool{
			"-n": true, "-i": true, "-l": true, "-c": true, "-F": true, "-w": true,
			"-S": true, "--smart-case": true, "--line-number": true, "--ignore-case": true,
			"--files": true, "--files-with-matches": true, "--count": true, "--hidden": true,
		}, map[string]bool{
			"-A": true, "-B": true, "-C": true, "-g": true, "--glob": true, "-t": true, "--type": true, "-m": true,
		})
	case "find":
		return isAllowedFindArgs(args)
	case "git":
		return isAllowedGitArgs(args)
	case "go":
		return isAllowedGoArgs(args)
	case "node", "npm", "pnpm", "yarn", "python", "python3", "rustc", "cargo", "tsc", "oxlint":
		return len(args) == 1 && (args[0] == "-v" || args[0] == "-V" || args[0] == "--version")
	default:
		return false
	}
}

func isAllowedLsArgs(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if arg != "--all" && arg != "--almost-all" && arg != "--human-readable" {
				return false
			}
			continue
		}
		for _, ch := range arg[1:] {
			if !strings.ContainsRune("laAhhrt1dFGSR", ch) {
				return false
			}
		}
	}
	return true
}

func isAllowedFindArgs(args []string) bool {
	allowedUnary := map[string]bool{
		"-not": true, "-and": true, "-or": true, "-a": true, "-o": true,
		"-print": true, "-print0": true, "-empty": true, "-L": true, "-P": true,
	}
	allowedValue := map[string]bool{
		"-name": true, "-iname": true, "-path": true, "-ipath": true,
		"-type": true, "-maxdepth": true, "-mindepth": true, "-size": true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if allowedUnary[arg] {
			continue
		}
		if allowedValue[arg] {
			i++
			continue
		}
		return false
	}
	return true
}

func isAllowedGoArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version":
		return allFlagsAllowed(args[1:], map[string]bool{"-m": true, "-v": true}, nil)
	case "env":
		// Explicitly disallow -w / -u which mutate Go environment config.
		return allFlagsAllowed(args[1:], map[string]bool{"-json": true}, nil)
	case "doc":
		return allFlagsAllowed(args[1:], map[string]bool{"-all": true, "-short": true, "-u": true, "-src": true}, nil)
	case "list":
		return allFlagsAllowed(args[1:], map[string]bool{"-m": true, "-json": true, "-deps": true}, map[string]bool{"-f": true})
	default:
		return false
	}
}

func isAllowedGitArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Reject global flags before subcommand (such as -c, --config-env, --git-dir, --work-tree)
	// except --version.
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		return true
	}
	if strings.HasPrefix(args[0], "-") {
		return false
	}

	sub := args[0]
	rest := args[1:]
	switch sub {
	case "status":
		return allFlagsAllowed(rest, map[string]bool{
			"-s": true, "--short": true, "-b": true, "--branch": true,
			"--porcelain": true, "-u": true, "--untracked-files": true,
		}, map[string]bool{"--untracked-files": true})
	case "log":
		return allFlagsAllowed(rest, map[string]bool{
			"--oneline": true, "--stat": true, "--graph": true, "--decorate": true,
			"--all": true, "-p": true, "--patch": true, "--name-only": true,
			"--name-status": true, "--no-merges": true, "--reverse": true,
		}, map[string]bool{
			"-n": true, "--max-count": true, "--format": true, "--pretty": true,
			"--since": true, "--until": true, "--author": true, "--grep": true,
		})
	case "diff":
		// Explicitly excludes --output and --ext-diff.
		return allFlagsAllowed(rest, map[string]bool{
			"--stat": true, "--name-only": true, "--name-status": true,
			"--cached": true, "--staged": true, "--numstat": true,
			"--shortstat": true, "--no-color": true, "--color": true,
			"--no-ext-diff": true,
		}, map[string]bool{
			"-U": true, "--unified": true,
		})
	case "show":
		return allFlagsAllowed(rest, map[string]bool{
			"--stat": true, "--name-only": true, "--name-status": true,
			"--oneline": true, "--no-patch": true, "-s": true,
		}, map[string]bool{
			"--format": true, "--pretty": true,
		})
	case "branch":
		// Only allow listing/querying flags; reject positional args (which create branches) and -d/-D/-m/-M.
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return false
			}
			switch a {
			case "-a", "--all", "-r", "--remotes", "-v", "-vv", "--list", "--show-current":
			default:
				return false
			}
		}
		return true
	case "rev-parse":
		return allFlagsAllowed(rest, map[string]bool{
			"--abbrev-ref": true, "--short": true, "--show-toplevel": true,
			"--is-inside-work-tree": true, "--git-dir": true,
		}, nil)
	case "remote":
		return len(rest) == 0 || (len(rest) == 1 && (rest[0] == "-v" || rest[0] == "--verbose"))
	case "tag":
		if len(rest) == 0 {
			return true
		}
		return len(rest) >= 1 && (rest[0] == "-l" || rest[0] == "--list") && allFlagsAllowed(rest[1:], nil, nil)
	default:
		return false
	}
}

// allFlagsAllowed verifies every flag argument is in either boolFlags or valueFlags.
// Supports --flag=value form for valueFlags and stops checking flags after "--".
func allFlagsAllowed(args []string, boolFlags map[string]bool, valueFlags map[string]bool) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return true
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if eqIdx := strings.IndexByte(arg, '='); eqIdx > 0 {
			flagName := arg[:eqIdx]
			if valueFlags[flagName] {
				continue
			}
			return false
		}
		if boolFlags[arg] {
			continue
		}
		if valueFlags[arg] {
			if i+1 >= len(args) {
				return false
			}
			i++
			continue
		}
		// Allow compact numeric forms like -n5 or -U3 when -n / -U is a valueFlag,
		// and combined short boolean flags like -rn when every individual flag is in boolFlags.
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			shortFlag := arg[:2]
			if valueFlags[shortFlag] && isDigits(arg[2:]) {
				continue
			}
			allShortBool := true
			for _, ch := range arg[1:] {
				if !boolFlags["-"+string(ch)] {
					allShortBool = false
					break
				}
			}
			if allShortBool {
				continue
			}
		}
		return false
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// splitShellWords splits a single shell command line respecting single/double quotes and backslash escapes.
// Returns (nil, false) on unterminated quotes or trailing escape.
func splitShellWords(s string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inToken := false
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			cur.WriteByte(c)
			inToken = true
			escaped = false
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		if inDouble {
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inDouble = false
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\\':
			escaped = true
			inToken = true
		case '\'':
			inSingle = true
			inToken = true
		case '"':
			inDouble = true
			inToken = true
		case ' ', '\t':
			if inToken {
				words = append(words, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	if inSingle || inDouble || escaped {
		return nil, false
	}
	if inToken {
		words = append(words, cur.String())
	}
	return words, true
}
