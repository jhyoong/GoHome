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
		{"sudo on second line", "echo hi\nsudo ls", true},
		{"sudo in command substitution", "x=$(sudo cat /etc/shadow)", true},
		{"sudo in subshell", "(sudo ls)", true},
		{"sudo after newline and spaces", "echo hi\n  sudo ls", true},
		{"pseudo in subshell", "(pseudo ls)", false},
		{"for loop body", "for s in a b; do sudo systemctl restart $s; done", true},
		{"if then", "if test -f x; then sudo rm x; fi", true},
		{"if else", "if test -f x; then echo ok; else sudo touch x; fi", true},
		{"elif", "if a; then b; elif sudo -n true; then c; fi", true},
		{"if condition", "if sudo -n true; then echo ok; fi", true},
		{"while condition", "while sudo -n true; do sleep 1; done", true},
		{"until condition", "until sudo -n true; do sleep 1; done", true},
		{"negated", "! sudo -n true", true},
		{"brace group", "{ sudo ls; }", true},
		{"backticks", "x=`sudo cat /etc/shadow`", true},
		{"time prefix", "time sudo apt update", true},
		{"env prefix", "env DEBIAN_FRONTEND=noninteractive sudo apt install -y vim", true},
		{"env no vars", "env sudo ls", true},
		{"keyword inside word", "redo sudoku", false},
		{"keyword without sudo", "for f in *; do echo $f; done", false},
		{"sudo as argument", "echo do sudoers", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSudoCommand(tt.command); got != tt.want {
				t.Errorf("IsSudoCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
