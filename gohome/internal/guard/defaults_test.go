package guard

import "testing"

func TestDefaultDenyPatterns_NotEmpty(t *testing.T) {
	if len(DefaultDenyPatterns) == 0 {
		t.Error("expected DefaultDenyPatterns to have entries")
	}
}

func TestDefaultDenyPatterns_ContainsSubstringAndRegex(t *testing.T) {
	hasSubstring := false
	hasRegex := false
	for _, p := range DefaultDenyPatterns {
		if len(p) > 6 && p[:6] == "regex:" {
			hasRegex = true
		} else {
			hasSubstring = true
		}
	}
	if !hasSubstring {
		t.Error("expected at least one substring pattern in defaults")
	}
	if !hasRegex {
		t.Error("expected at least one regex pattern in defaults")
	}
}

func defaultDenylist(t *testing.T) *Denylist {
	t.Helper()
	dl, err := CompileDenylist(DenylistFile{Shell: DefaultDenyPatterns})
	if err != nil {
		t.Fatalf("CompileDenylist: %v", err)
	}
	return dl
}

func TestDefaultDenyPatterns_DeniesBypassCases(t *testing.T) {
	dl := defaultDenylist(t)
	cmds := []string{
		"rm -rf /",
		"rm -fr /",
		"rm -r -f /",
		"rm  -rf /",
		"rm\t-rf\t/",
		`rm -rf "/"`,
		"rm -rf '/'",
		"rm -rf ~",
		"rm -rf ~/",
		"rm -rf /*",
		"rm -rf $HOME",
		"rm --recursive --force /",
		"rm -rf --no-preserve-root /",
		"sudo rm -rf /",
		"cd x; rm -rf /",
		"true && rm -rf / && true",
		"rm -rf / --no-preserve-root",
		"mkfs.ext4 /dev/sda1",
		"mkfs /dev/sda1",
		"sudo mkfs.ext4 /dev/sda1",
		"ls; mkfs /dev/sda1",
		"ls\nmkfs /dev/sda1",
		"ls; rm -rf /",
		"ls\nrm -rf /",
		"(rm -rf /)",
		"sudo -n rm -rf /",
		"sudo -n mkfs /dev/sda1",
		"env rm -rf /",
		"env FOO=1 rm -rf ~",
		"time rm -rf /",
		"nice -n 5 rm -rf /",
		"sudo -n /usr/bin/rm -rf /",
		"/bin/rm -rf /",
		"sudo /bin/rm -rf /",
		":(){ :|:& };:",
		"echo x > /dev/sda",
	}
	for _, c := range cmds {
		if ok, _ := dl.Denies("shell", shellInput(c)); !ok {
			t.Errorf("expected %q to be denied", c)
		}
	}
}

func TestDefaultDenyPatterns_AllowsSafeCases(t *testing.T) {
	dl := defaultDenylist(t)
	cmds := []string{
		"rm -rf /tmp/build",
		"rm -rf ./build",
		"rm -rf ~/build",
		"rm -rf $HOME/build",
		"rm -f /tmp/x",
		"rm /",
		"rm -r foo; ls /",
		"rm -r foo && ls /",
		"echo mkfs",
		"echo rm -rf /",
		"sudo echo rm -rf /",
		"man rm",
		"man rm -rf /",
		"man mkfs",
		"ls /",
	}
	for _, c := range cmds {
		if ok, p := dl.Denies("shell", shellInput(c)); ok {
			t.Errorf("expected %q to be allowed, matched %q", c, p)
		}
	}
}
