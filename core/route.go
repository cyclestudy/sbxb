package core

import (
	"encoding/json"
	"log/slog"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/cyclestudy/sbxb/api/xboard"
)

// RouteResult holds the output of BuildRouteRules.
type RouteResult struct {
	Rules     []option.Rule
	RuleSets  []option.RuleSet
	Outbounds []option.Outbound
	Final     string // default outbound tag, empty = "direct"
	NeedSniff bool   // whether protocol sniffing is required
}

// BuildRouteRules converts XBoard panel routes into sing-box route rules,
// extra outbounds (for default_out), and the final outbound tag.
func BuildRouteRules(routes []xboard.Route) RouteResult {
	var result RouteResult

	for _, r := range routes {
		switch r.Action {
		case "block":
			if len(r.Match) == 0 {
				continue
			}
			result.Rules = append(result.Rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						DomainSuffix: badoption.Listable[string](r.Match),
					},
					RuleAction: blockAction(),
				},
			})
			slog.Info("route rule: block domains", "id", r.ID, "count", len(r.Match))

		case "block_ip":
			if len(r.Match) == 0 {
				continue
			}
			// Parse "geoip:XX" format → use rule_set (geoip db removed in sing-box 1.12).
			for _, m := range r.Match {
				if strings.HasPrefix(m, "geoip:") {
					code := strings.TrimPrefix(m, "geoip:")
					tag := "geoip-" + code
					result.RuleSets = append(result.RuleSets, option.RuleSet{
						Type:   C.RuleSetTypeRemote,
						Tag:    tag,
						Format: C.RuleSetFormatBinary,
						RemoteOptions: option.RemoteRuleSet{
							URL:            "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-" + code + ".srs",
							DownloadDetour: "direct",
						},
					})
					result.Rules = append(result.Rules, option.Rule{
						Type: C.RuleTypeDefault,
						DefaultOptions: option.DefaultRule{
							RawDefaultRule: option.RawDefaultRule{
								RuleSet: badoption.Listable[string]{tag},
							},
							RuleAction: blockAction(),
						},
					})
					slog.Info("route rule: block geoip via rule_set", "id", r.ID, "code", code)
				} else {
					// Plain IP CIDR.
					result.Rules = append(result.Rules, option.Rule{
						Type: C.RuleTypeDefault,
						DefaultOptions: option.DefaultRule{
							RawDefaultRule: option.RawDefaultRule{
								IPCIDR: badoption.Listable[string]{m},
							},
							RuleAction: blockAction(),
						},
					})
					slog.Info("route rule: block ip cidr", "id", r.ID, "cidr", m)
				}
			}

		case "protocol":
			if len(r.Match) == 0 {
				continue
			}
			result.NeedSniff = true
			result.Rules = append(result.Rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						Protocol: badoption.Listable[string](r.Match),
					},
					RuleAction: blockAction(),
				},
			})
			slog.Info("route rule: block protocols", "id", r.ID, "protocols", r.Match)

		case "direct":
			if len(r.Match) == 0 {
				continue
			}
			result.Rules = append(result.Rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						DomainSuffix: badoption.Listable[string](r.Match),
					},
					RuleAction: option.RuleAction{
						Action: C.RuleActionTypeRoute,
						RouteOptions: option.RouteActionOptions{
							Outbound: "direct",
						},
					},
				},
			})
			slog.Info("route rule: direct domains", "id", r.ID, "count", len(r.Match))

		case "default_out":
			outbound, tag := parseDefaultOut(r.ActionValue)
			if outbound != nil {
				result.Outbounds = append(result.Outbounds, *outbound)
				result.Final = tag
				slog.Info("route rule: default outbound", "id", r.ID, "tag", tag)
			}

		default:
			slog.Warn("unknown route action, skipping", "action", r.Action, "id", r.ID)
		}
	}

	return result
}

func blockAction() option.RuleAction {
	return option.RuleAction{
		Action: C.RuleActionTypeRoute,
		RouteOptions: option.RouteActionOptions{
			Outbound: "block",
		},
	}
}

// v2rayOutConfig is the format XBoard uses for default_out action_value.
type v2rayOutConfig struct {
	Protocol string `json:"protocol"`
	Tag      string `json:"tag"`
	Settings struct {
		Servers []struct {
			Address string `json:"address"`
			Port    uint16 `json:"port"`
			Users   []struct {
				User string `json:"user"`
				Pass string `json:"pass"`
			} `json:"users"`
		} `json:"servers"`
	} `json:"settings"`
}

func parseDefaultOut(actionValue string) (*option.Outbound, string) {
	if actionValue == "" {
		return nil, ""
	}

	var cfg v2rayOutConfig
	if err := json.Unmarshal([]byte(actionValue), &cfg); err != nil {
		slog.Error("failed to parse default_out config", "error", err)
		return nil, ""
	}

	tag := cfg.Tag
	if tag == "" {
		tag = "default-out"
	}

	switch cfg.Protocol {
	case "socks":
		if len(cfg.Settings.Servers) == 0 {
			slog.Error("default_out socks: no servers")
			return nil, ""
		}
		srv := cfg.Settings.Servers[0]
		opts := &option.SOCKSOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     srv.Address,
				ServerPort: srv.Port,
			},
		}
		if len(srv.Users) > 0 {
			opts.Username = srv.Users[0].User
			opts.Password = srv.Users[0].Pass
		}
		return &option.Outbound{
			Type:    C.TypeSOCKS,
			Tag:     tag,
			Options: opts,
		}, tag

	case "http":
		if len(cfg.Settings.Servers) == 0 {
			slog.Error("default_out http: no servers")
			return nil, ""
		}
		srv := cfg.Settings.Servers[0]
		opts := &option.HTTPOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     srv.Address,
				ServerPort: srv.Port,
			},
		}
		if len(srv.Users) > 0 {
			opts.Username = srv.Users[0].User
			opts.Password = srv.Users[0].Pass
		}
		return &option.Outbound{
			Type:    C.TypeHTTP,
			Tag:     tag,
			Options: opts,
		}, tag

	default:
		slog.Error("unsupported default_out protocol", "protocol", cfg.Protocol)
		return nil, ""
	}
}
