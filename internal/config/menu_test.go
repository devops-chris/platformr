package config

import (
	"strings"
	"testing"
)

func names(rs []Resource) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return strings.Join(out, ",")
}

func TestMenuCategories(t *testing.T) {
	resources := []Resource{
		{Name: "account", Category: "Platform team"},
		{Name: "misc"},
		{Name: "eks", Category: "Kubernetes"},
		{Name: "vpc", Category: "Network"},
		{Name: "other", Category: "Extras"},
	}
	declared := []CategoryConfig{
		{Name: "Network", Description: "VPCs"},
		{Name: "Unused", Description: "no requests, so hidden"},
		{Name: "Kubernetes"},
		{Name: "Network"}, // listed twice: first wins
	}
	var got []string
	for _, c := range MenuCategories(resources, declared) {
		got = append(got, c.Name+"="+c.Description)
	}
	want := "Network=VPCs,Kubernetes=,Platform team=,Extras=,General="
	if strings.Join(got, ",") != want {
		t.Errorf("got %s\nwant %s", strings.Join(got, ","), want)
	}
}

func TestInMenuOrder(t *testing.T) {
	resources := []Resource{
		{Name: "eks-upgrade", Category: "Kubernetes", Action: "change"},
		{Name: "eks", Category: "Kubernetes"},
		{Name: "vpc", Category: "Network"},
		{Name: "eks-addon", Category: "Kubernetes", Action: "create"},
	}
	creates, changes := InMenuOrder(resources, "Kubernetes")
	if names(creates) != "eks,eks-addon" || names(changes) != "eks-upgrade" {
		t.Errorf("creates = %s, changes = %s", names(creates), names(changes))
	}
	if c, ch := InMenuOrder([]Resource{{Name: "x"}}, Uncategorized); names(c) != "x" || len(ch) != 0 {
		t.Errorf("uncategorized request not in %q", Uncategorized)
	}
}
