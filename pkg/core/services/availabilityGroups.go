package services

import (
	"sort"
	"strings"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// A group is the atomic unit of allocation: its members are placed together, so
// availability is a group property even though requests are per volunteer. The
// bridge between the two grains lives here and nowhere else — the equivalent
// logic on the Forms path is written three times over (viewResponses.go,
// allocationHelpers.go, allocator/init.go), which is what this replaces as those
// consumers move across in slice 4 (ADR 0004).

// groupKey returns the key binding a volunteer to those allocated alongside
// them. A volunteer with no group on the sheet is their own group of one, keyed
// on their id so they can never collide with a real group.
func groupKey(v model.Volunteer) string {
	if v.GroupKey == "" {
		return "individual:" + v.ID
	}
	return v.GroupKey
}

// groupPartners returns the other volunteers sharing v's group, in a stable
// order. Empty for someone with no group. matching, when non-nil, keeps only
// partners it accepts.
func groupPartners(volunteers []model.Volunteer, v model.Volunteer, matching func(model.Volunteer) bool) []model.Volunteer {
	key := groupKey(v)
	partners := make([]model.Volunteer, 0)
	for _, other := range volunteers {
		if other.ID == v.ID || groupKey(other) != key {
			continue
		}
		if matching != nil && !matching(other) {
			continue
		}
		partners = append(partners, other)
	}
	sort.Slice(partners, func(i, j int) bool { return volunteerName(partners[i]) < volunteerName(partners[j]) })
	return partners
}

// partnerNames names a set of group partners for display.
func partnerNames(partners []model.Volunteer) []string {
	names := make([]string, 0, len(partners))
	for _, p := range partners {
		names = append(names, volunteerName(p))
	}
	return names
}

// volunteerName is the full name — first plus last — which is how both the
// volunteer's form and the admin roster address someone. Not the display name:
// a wrong name is how a volunteer notices a forwarded link, and the shortest
// unambiguous form is a weaker signal than the whole thing.
func volunteerName(v model.Volunteer) string {
	return strings.TrimSpace(v.FirstName + " " + v.LastName)
}
