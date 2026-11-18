package main

import (
	"strings"
	"testing"
)

func TestFromImage(t *testing.T) {
	df := []byte("# escape=`\nARG BASE_TAG=ltsc2022\nFROM mcr.microsoft.com/windows/server:${BASE_TAG}\nRUN echo\n")
	if got := fromImage(df, []string{"BASE_TAG=ltsc2025"}); got != "mcr.microsoft.com/windows/server:ltsc2025" {
		t.Errorf("fromImage = %q", got)
	}
	df = []byte("from --platform=windows/amd64 mcr.microsoft.com/windows/servercore:$BASE_TAG\n")
	if got := fromImage(df, []string{"BASE_TAG=ltsc2022"}); got != "mcr.microsoft.com/windows/servercore:ltsc2022" {
		t.Errorf("fromImage（--platform・$KEY）= %q", got)
	}
}

func TestStaleWinBases(t *testing.T) {
	refs := []string{
		"localhost/koya/base:latest",
		"mcr.microsoft.com/windows/server:ltsc2025",
		"mcr.microsoft.com/windows/servercore:ltsc2025",
		"mcr.microsoft.com/windows/nanoserver:<none>",
		"<none>:<none>",
		"mcr.microsoft.com/dotnet/sdk:10.0",
	}
	got := strings.Join(staleWinBases(refs, "mcr.microsoft.com/windows/server:ltsc2025"), ",")
	if got != "mcr.microsoft.com/windows/servercore:ltsc2025" {
		t.Errorf("staleWinBases = %q", got)
	}
}

func TestProfilesOfRepos(t *testing.T) {
	got := strings.Join(profilesOfRepos([]string{"localhost/koya/node", "<none>", "koya/base", "localhost/koya/node", "docker.io/library/alpine"}), ",")
	if got != "base,node" {
		t.Errorf("profilesOfRepos = %q", got)
	}
}
