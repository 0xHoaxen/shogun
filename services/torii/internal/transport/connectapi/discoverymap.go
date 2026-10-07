package connectapi

import (
	"strings"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
)

// The API's source kinds carry a DISCOVERY_ prefix shinobi's do not, so they are
// converted by name: a value one side lacks becomes unspecified instead of
// silently turning into a different one.
const discoveryPrefix = "DISCOVERY_"

func sourceKindToAPI(k shinobiv1.SourceKind) apiv1.DiscoverySourceKind {
	return apiv1.DiscoverySourceKind(apiv1.DiscoverySourceKind_value[discoveryPrefix+k.String()])
}

func sourceKindToShinobi(k apiv1.DiscoverySourceKind) shinobiv1.SourceKind {
	return shinobiv1.SourceKind(shinobiv1.SourceKind_value[strings.TrimPrefix(k.String(), discoveryPrefix)])
}

func discoveryMappingToAPI(m *shinobiv1.FieldMapping) *apiv1.DiscoveryFieldMapping {
	if m == nil {
		return nil
	}
	return &apiv1.DiscoveryFieldMapping{
		ItemsPath: m.GetItemsPath(), Id: m.GetId(), Title: m.GetTitle(), Company: m.GetCompany(), Url: m.GetUrl(),
		Location: m.GetLocation(), PostedAt: m.GetPostedAt(), Description: m.GetDescription(),
	}
}

func discoveryMappingToShinobi(m *apiv1.DiscoveryFieldMapping) *shinobiv1.FieldMapping {
	if m == nil {
		return nil
	}
	return &shinobiv1.FieldMapping{
		ItemsPath: m.GetItemsPath(), Id: m.GetId(), Title: m.GetTitle(), Company: m.GetCompany(), Url: m.GetUrl(),
		Location: m.GetLocation(), PostedAt: m.GetPostedAt(), Description: m.GetDescription(),
	}
}

func discoverySourceToAPI(s *shinobiv1.Source) *apiv1.DiscoverySource {
	return &apiv1.DiscoverySource{
		Id: s.GetId(), Name: s.GetName(), Kind: sourceKindToAPI(s.GetKind()), Schedule: s.GetSchedule(), Enabled: s.GetEnabled(),
		LastRunAt: s.GetLastRunAt(), LastError: s.GetLastError(),
		Config: &apiv1.DiscoverySourceConfig{
			Url: s.GetConfig().GetUrl(), Document: s.GetConfig().GetDocument(), Mapping: discoveryMappingToAPI(s.GetConfig().GetMapping()),
		},
	}
}

func discoverySourceToShinobi(s *apiv1.DiscoverySource) *shinobiv1.Source {
	return &shinobiv1.Source{
		Id: s.GetId(), Name: s.GetName(), Kind: sourceKindToShinobi(s.GetKind()), Schedule: s.GetSchedule(), Enabled: s.GetEnabled(),
		Config: &shinobiv1.SourceConfig{
			Url: s.GetConfig().GetUrl(), Document: s.GetConfig().GetDocument(), Mapping: discoveryMappingToShinobi(s.GetConfig().GetMapping()),
		},
	}
}

func discoveryPreferencesToAPI(p *shinobiv1.Preferences) *apiv1.DiscoveryPreferences {
	return &apiv1.DiscoveryPreferences{
		Roles: p.GetRoles(), Locations: p.GetLocations(), MustHave: p.GetMustHave(), NiceToHave: p.GetNiceToHave(),
		Exclude: p.GetExclude(), MinScore: p.GetMinScore(),
	}
}

func discoveryPreferencesToShinobi(p *apiv1.DiscoveryPreferences) *shinobiv1.Preferences {
	return &shinobiv1.Preferences{
		Roles: p.GetRoles(), Locations: p.GetLocations(), MustHave: p.GetMustHave(), NiceToHave: p.GetNiceToHave(),
		Exclude: p.GetExclude(), MinScore: p.GetMinScore(),
	}
}

func discoveryPostingToAPI(p *shinobiv1.Posting) *apiv1.DiscoveryPosting {
	return &apiv1.DiscoveryPosting{
		Id: p.GetId(), SourceId: p.GetSourceId(), Title: p.GetTitle(), Company: p.GetCompany(), Url: p.GetUrl(),
		Location: p.GetLocation(), PostedAt: p.GetPostedAt(), Scored: p.GetScored(), Score: p.GetScore(),
		Reasons: p.GetReasons(), ScoredBy: p.GetScoredBy(), SavedJobId: p.GetSavedJobId(), CreatedAt: p.GetCreatedAt(),
	}
}
