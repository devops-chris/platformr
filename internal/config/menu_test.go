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

func TestInMenuOrderGuideThenItsSteps(t *testing.T) {
	rs := []Resource{
		{Name: "ecr", Category: "Env"},
		{Name: "eks", Category: "Env"},
		{Name: "other", Category: "Env"},
		{Name: "env", Category: "Env", Steps: []StepConfig{{Request: "vpc"}, {Request: "eks"}, {Request: "elsewhere"}, {Request: "ecr"}}},
		{Name: "vpc", Category: "Env"},
		{Name: "upgrade", Category: "Env", Action: "change"},
		{Name: "elsewhere", Category: "Other"},
	}
	creates, changes := InMenuOrder(rs, "Env")
	var got []string
	for _, r := range creates {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != "env,vpc,eks,ecr,other" {
		t.Errorf("creates = %v, want the guide, its steps in step order (only this category's), then the rest", got)
	}
	if len(changes) != 1 || changes[0].Name != "upgrade" {
		t.Errorf("changes = %v", changes)
	}

	// No guide: definition order, as before.
	creates, _ = InMenuOrder(rs[:3], "Env")
	if creates[0].Name != "ecr" || creates[2].Name != "other" {
		t.Errorf("without a guide the order shouldn't change: %v", creates)
	}
}
