package core

import (
	"log/slog"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/cyclestudy/sbxb/api/xboard"
)

// BuildRouteRules converts XBoard panel routes into sing-box route rules.
func BuildRouteRules(routes []xboard.Route) []option.Rule {
	var rules []option.Rule

	for _, r := range routes {
		if len(r.Match) == 0 {
			continue
		}

		domains := badoption.Listable[string](r.Match)

		var action option.RuleAction
		switch r.Action {
		case "block":
			action = option.RuleAction{
				Action: C.RuleActionTypeRoute,
				RouteOptions: option.RouteActionOptions{
					Outbound: "block",
				},
			}
		case "direct":
			action = option.RuleAction{
				Action: C.RuleActionTypeRoute,
				RouteOptions: option.RouteActionOptions{
					Outbound: "direct",
				},
			}
		default:
			slog.Warn("unknown route action, skipping",
				"action", r.Action,
				"id", r.ID,
			)
			continue
		}

		rule := option.Rule{
			Type: C.RuleTypeDefault,
			DefaultOptions: option.DefaultRule{
				RawDefaultRule: option.RawDefaultRule{
					DomainSuffix: domains,
				},
				RuleAction: action,
			},
		}
		rules = append(rules, rule)

		slog.Info("route rule added",
			"id", r.ID,
			"action", r.Action,
			"domains", len(r.Match),
		)
	}

	return rules
}
