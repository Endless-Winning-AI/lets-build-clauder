package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The Claude desktop app keeps one JSON file per Code-tab chat. Its sidebar
// lists them grouped by project, A to Z, with "This computer" (chats with no
// folder) last; newest first within a group, each chat's side chats under it.

const thisComputer = "This computer"

// chatFile is the part of a desktop chat's file the sidebar order needs.
type chatFile struct {
	SessionID      string `json:"sessionId"`
	Cwd            string `json:"cwd"`
	IsArchived     bool   `json:"isArchived"`
	LastActivityAt int64  `json:"lastActivityAt"`
	LastFocusedAt  int64  `json:"lastFocusedAt"`
	GitAnchors     []struct {
		GitRoot   string `json:"gitRoot"`
		CommonDir string `json:"commonDir"`
	} `json:"gitAnchors"`
	SpawnedFrom *struct {
		SessionID string `json:"sessionId"`
	} `json:"spawnedFrom"`
}

// sidebarDir is where the desktop app keeps its chats, one folder per account.
func sidebarDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
}

// readChats reads every chat the sidebar shows: those of the account
// folder used last, as older sign-ins leave folders of their own.
func readChats(dir string) []chatFile {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*", "local_*.json"))
	account, newest := "", int64(0)
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.ModTime().UnixNano() > newest {
			account, newest = filepath.Dir(p), info.ModTime().UnixNano()
		}
	}
	var chats []chatFile
	for _, p := range paths {
		if filepath.Dir(p) != account {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var c chatFile
		if json.Unmarshal(data, &c) != nil || c.SessionID == "" || c.IsArchived {
			continue
		}
		chats = append(chats, c)
	}
	return chats
}

// sidebarOrder is the chats' host ids in the order the sidebar lists them,
// and the one last brought forward.
func sidebarOrder(dir string, hide []string) (order []string, shown string) {
	chats := readChats(dir)
	byID := map[string]bool{}
	for _, c := range chats {
		byID[c.SessionID] = true
	}
	hidden := map[string]bool{}
	for _, h := range hide {
		hidden[strings.ToLower(h)] = true
	}
	groups := map[string][]chatFile{}
	children := map[string][]chatFile{}
	var focused int64
	remotes := map[string]string{}
	for _, c := range chats {
		if c.LastFocusedAt > focused {
			focused, shown = c.LastFocusedAt, c.SessionID
		}
		if c.SpawnedFrom != nil && byID[c.SpawnedFrom.SessionID] {
			children[c.SpawnedFrom.SessionID] = append(children[c.SpawnedFrom.SessionID], c)
			continue
		}
		g := projectOf(c, remotes)
		if !hidden[strings.ToLower(g)] {
			groups[g] = append(groups[g], c)
		}
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == thisComputer) != (names[j] == thisComputer) {
			return names[j] == thisComputer
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	newest := func(cs []chatFile) {
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].LastActivityAt > cs[j].LastActivityAt })
	}
	var add func(c chatFile)
	add = func(c chatFile) {
		order = append(order, c.SessionID)
		kids := children[c.SessionID]
		newest(kids)
		for _, k := range kids {
			add(k)
		}
	}
	for _, g := range names {
		newest(groups[g])
		for _, c := range groups[g] {
			add(c)
		}
	}
	return order, shown
}

// projectOf is the sidebar group a chat is under: its repository's name,
// else its folder's, else "This computer" for a chat with no folder.
func projectOf(c chatFile, remotes map[string]string) string {
	if strings.Contains(c.Cwd, "/scratch-workspaces/") || c.Cwd == "" {
		return thisComputer
	}
	if len(c.GitAnchors) > 0 {
		a := c.GitAnchors[0]
		name, ok := remotes[a.CommonDir]
		if !ok {
			name = originName(filepath.Join(a.CommonDir, "config"))
			remotes[a.CommonDir] = name
		}
		if name != "" {
			return name
		}
		return filepath.Base(a.GitRoot)
	}
	if root := repoOf(c.Cwd); root != "" {
		name, ok := remotes[root]
		if !ok {
			name = originName(filepath.Join(root, ".git", "config"))
			remotes[root] = name
		}
		if name != "" {
			return name
		}
		return filepath.Base(root)
	}
	return filepath.Base(c.Cwd)
}

// repoOf is the main repository a folder is in (past any worktree inside
// it), or "".
func repoOf(dir string) string {
	for d := dir; d != filepath.Dir(d); d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, ".git")); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

// originName is the repository name in a git config's origin url, or "".
func originName(config string) string {
	f, err := os.Open(config)
	if err != nil {
		return ""
	}
	defer f.Close()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if key, value, ok := strings.Cut(line, "="); inOrigin && ok && strings.TrimSpace(key) == "url" {
			url := strings.TrimSuffix(strings.TrimSpace(value), "/")
			url = url[strings.LastIndexAny(url, "/:")+1:]
			return strings.TrimSuffix(url, ".git")
		}
	}
	return ""
}
