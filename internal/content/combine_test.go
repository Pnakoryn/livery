package content

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/exit"
)

func it(k Kind, layer, name string) Item {
	o := map[Kind]string{Skill: "skills/", InstructionKind: "instructions/", MCP: "mcp/"}[k] + name
	if k == MCP {
		o += ".json"
	}
	return Item{Kind: k, Name: name, Layer: layer, Origin: o}
}

func native(layer, name string) Item {
	return Item{Kind: Native, Harness: "kiro", Name: name, Layer: layer, Origin: "kiro/" + name}
}

func TestCombineOverrides(t *testing.T) {
	base := []Item{it(Skill, "base", "review"), it(InstructionKind, "base", "go")}
	web := []Item{it(Skill, "team:web", "review")}
	pers := []Item{it(InstructionKind, "personal", "go")}
	items, ov, err := Combine(base, [][]Item{web}, pers, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Layer != "personal" || items[1].Layer != "team:web" {
		t.Fatalf("items: %+v", items)
	}
	if len(ov) != 2 || ov[0].Winner.Layer != "personal" || ov[0].Loser.Layer != "base" ||
		ov[1].Winner.Layer != "team:web" || ov[1].Loser.Layer != "base" {
		t.Fatalf("overrides: %+v", ov)
	}
}

func TestCombineTeamClash(t *testing.T) {
	web := []Item{it(MCP, "team:web", "github")}
	api := []Item{it(MCP, "team:api", "github")}
	{
		_, _, err := Combine(nil, [][]Item{web, api}, nil, true)
		var ee *exit.Error
		if !errors.As(err, &ee) || ee.Code != exit.Finding {
			t.Fatalf("err = %v", err)
		}
		for _, w := range []string{"team:web: mcp/github.json", "team:api: mcp/github.json"} {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%q missing in %q", w, err)
			}
		}
	}
}

func TestCombineNative(t *testing.T) {
	items, ov, err := Combine([]Item{native("base", "hooks/a.json")}, [][]Item{{native("team:web", "hooks/a.json"), it(Skill, "team:web", "hooks/a.json")}}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(ov) != 1 || ov[0].Winner.Layer != "team:web" || ov[0].Winner.Kind != Native {
		t.Fatalf("items %+v overrides %+v", items, ov)
	}
}

func TestCombineTeamOrder(t *testing.T) {
	a := []Item{it(Skill, "team:a", "x")}
	b := []Item{it(Skill, "team:b", "y")}
	i1, _, err1 := Combine(nil, [][]Item{a, b}, nil, true)
	i2, _, err2 := Combine(nil, [][]Item{b, a}, nil, true)
	if err1 != nil || err2 != nil || !reflect.DeepEqual(i1, i2) {
		t.Fatalf("%v %v %+v %+v", err1, err2, i1, i2)
	}
}

func TestCombineEmpty(t *testing.T) {
	items, ov, err := Combine(nil, nil, nil, true)
	if err != nil || len(items) != 0 || len(ov) != 0 {
		t.Fatalf("%v %v %v", items, ov, err)
	}
}

func TestCombineTeamClashPersonalWins(t *testing.T) {
	web := []Item{it(MCP, "team:web", "github")}
	api := []Item{it(MCP, "team:api", "github")}
	items, ov, err := Combine(nil, [][]Item{web, api}, []Item{it(MCP, "personal", "github")}, true)
	if err != nil || len(items) != 1 || items[0].Layer != "personal" || len(ov) != 2 {
		t.Fatalf("items %+v ov %+v err %v", items, ov, err)
	}
}
