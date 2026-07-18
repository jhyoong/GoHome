package guard

import "testing"

func TestIsSudoCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"plain sudo", "sudo apt install vim", true},
		{"sudo with path", "sudo /usr/bin/apt install vim", true},
		{"piped sudo", "echo foo | sudo tee /etc/bar", true},
		{"chained sudo", "cd /tmp && sudo rm -rf stuff", true},
		{"semicolon sudo", "echo hi; sudo ls", true},
		{"or sudo", "test -f foo || sudo install foo", true},
		{"not sudo", "echo sudoers", false},
		{"grep sudoers", "grep sudo /etc/sudoers", false},
		{"empty", "", false},
		{"no sudo", "ls -la", false},
		{"sudo alone", "sudo", true},
		{"sudo-S already", "sudo -S apt install vim", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSudoCommand(tt.command); got != tt.want {
				t.Errorf("IsSudoCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
