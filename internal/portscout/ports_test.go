package portscout

import (
	"slices"
	"strings"
	"testing"
)

func TestParsePorts(t *testing.T) {
	tests := []struct {
		spec    string
		want    []int
		wantErr string
	}{
		{spec: "80", want: []int{80}},
		{spec: "443,80,22", want: []int{22, 80, 443}},
		{spec: " 22 , 80-82 ,  8080 ", want: []int{22, 80, 81, 82, 8080}},
		{spec: "80 - 82", want: []int{80, 81, 82}},
		{spec: "80,80,79-81", want: []int{79, 80, 81}},
		{spec: "1,65535", want: []int{1, 65535}},
		{spec: "5-5", want: []int{5}},
		{spec: "80,", want: []int{80}},
		{spec: ",,22,,", want: []int{22}},
		{spec: "common", want: CommonPorts()},
		{spec: "COMMON,1", want: append([]int{1}, CommonPorts()...)},
		{spec: "", wantErr: "is empty"},
		{spec: " , ", wantErr: "is empty"},
		{spec: "0", wantErr: "out of range"},
		{spec: "65536", wantErr: "out of range"},
		{spec: "-80", wantErr: "out of range"},
		{spec: "http", wantErr: "not a number"},
		{spec: "80-70", wantErr: "start 80 is greater than end 70"},
		{spec: "80-", wantErr: "missing bound"},
		{spec: "1-2-3", wantErr: "not a number"},
		{spec: "1-70000", wantErr: "out of range"},
		{spec: "22,abc", wantErr: `invalid port "abc"`},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParsePorts(tt.spec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParsePorts(%q) error = %v, want containing %q", tt.spec, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePorts(%q): %v", tt.spec, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ParsePorts(%q) = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestParsePortsAll(t *testing.T) {
	got, err := ParsePorts("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxPort || got[0] != 1 || got[len(got)-1] != MaxPort {
		t.Fatalf("all: len=%d first=%d last=%d", len(got), got[0], got[len(got)-1])
	}
}

func TestCommonPorts(t *testing.T) {
	ports := CommonPorts()
	if len(ports) < 25 {
		t.Fatalf("curated list too short: %d", len(ports))
	}
	if !slices.IsSorted(ports) {
		t.Fatal("curated list must be sorted")
	}
	for _, p := range ports {
		if ServiceName(p) == "" {
			t.Errorf("curated port %d has no service name", p)
		}
	}
	ports[0] = -1
	if CommonPorts()[0] == -1 {
		t.Fatal("CommonPorts must return a copy")
	}
}

func TestServiceName(t *testing.T) {
	for port, want := range map[int]string{22: "ssh", 5432: "postgres", 6379: "redis", 3389: "rdp", 12345: ""} {
		if got := ServiceName(port); got != want {
			t.Errorf("ServiceName(%d) = %q, want %q", port, got, want)
		}
	}
}
