package content

import (
	"sort"
	"strings"

	"github.com/Pnakoryn/livery/internal/exit"
)

// Override records that Winner, from a later layer, replaced Loser with the same identity.
type Override struct{ Winner, Loser Item }

type identity struct {
	kind    Kind
	harness string
	name    string
}

func identityOf(it Item) identity {
	id := identity{kind: it.Kind, name: it.Name}
	if it.Kind == Native {
		id.harness = it.Harness
	}
	return id
}

// level collects the items of one level by identity. An identity held twice is a clash unless
// personal defines it too: then personal wins and both are recorded as overrides.
func level(groups [][]Item, personal map[identity]Item, what, hint string) (items []Item, extra []Override, clashes []string) {
	by := map[identity][]Item{}
	var order []identity
	for _, g := range groups {
		for _, it := range g {
			id := identityOf(it)
			if _, ok := by[id]; !ok {
				order = append(order, id)
			}
			by[id] = append(by[id], it)
		}
	}
	for _, id := range order {
		its := by[id]
		if len(its) == 1 {
			items = append(items, its[0])
			continue
		}
		if w, ok := personal[id]; ok {
			for _, it := range its {
				extra = append(extra, Override{Winner: w, Loser: it})
			}
			continue
		}
		parts := make([]string, len(its))
		for i, it := range its {
			parts[i] = it.Layer + ": " + it.Origin
		}
		sort.Strings(parts)
		clashes = append(clashes, strings.Join(parts, " and ")+": the same item in two "+what+"; ask the owners to remove it from one"+hint)
	}
	return items, extra, clashes
}

// Combine merges the layers: base (level 1), the teams in config order (level 2), then
// personal (level 3). A later level replaces an earlier item of the same identity and the
// replacement is recorded as an override. The same identity in two teams is an exit.Finding
// error, unless personal defines that item too. hasPersonal says whether there is a personal
// layer to point to: when false (a project), clash messages end without the hint to it.
// base, teams and personal may be nil.
func Combine(base []Item, teams [][]Item, personal []Item, hasPersonal bool) (items []Item, overrides []Override, err error) {
	hint := ""
	if hasPersonal {
		hint = ", or put your own version into the personal layer"
	}
	pers := map[identity]Item{}
	for _, it := range personal {
		pers[identityOf(it)] = it
	}
	team, extraT, clashT := level(teams, pers, "teams", hint)
	if len(clashT) > 0 {
		sort.Strings(clashT)
		return nil, nil, exit.Findingf("%s", strings.Join(clashT, "\n"))
	}

	current := map[identity]Item{}
	apply := func(lv []Item) {
		for _, it := range lv {
			id := identityOf(it)
			if old, ok := current[id]; ok {
				overrides = append(overrides, Override{Winner: it, Loser: old})
			}
			current[id] = it
		}
	}
	apply(base)
	SortItems(team)
	apply(team)
	apply(personal)
	overrides = append(overrides, extraT...)

	for _, it := range current {
		items = append(items, it)
	}
	SortItems(items)
	sort.SliceStable(overrides, func(i, j int) bool { return ItemLess(overrides[i].Winner, overrides[j].Winner) })
	return items, overrides, nil
}
