package desktop

// Detect returns the desktop this session runs on.
func Detect(runtimeDir string) Desktop {
	if h := NewHyprland(runtimeDir); h != nil {
		return h
	}
	return None{}
}
