package config

import "testing"

func TestValidTeam(t *testing.T) {
	for s, want := range map[string]bool{
		"web": true, "platform-2": true, "0ops": true,
		"": false, "-web": false, "My_Team": false, "web team": false, "Web": false,
	} {
		if got := ValidTeam(s); got != want {
			t.Errorf("ValidTeam(%q) = %v, want %v", s, got, want)
		}
	}
}
