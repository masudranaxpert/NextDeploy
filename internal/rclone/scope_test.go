package rclone

import "testing"

func TestHasFullDriveScope(t *testing.T) {
	cases := map[string]bool{
		"": true,
		"https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/userinfo.email": true,
		"https://www.googleapis.com/auth/userinfo.email openid":                                false,
		"https://www.googleapis.com/auth/drive.file":                                           false,
	}
	for scope, want := range cases {
		if got := hasFullDriveScope(scope); got != want {
			t.Errorf("hasFullDriveScope(%q) = %v, want %v", scope, got, want)
		}
	}
}
