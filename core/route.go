package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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

	// DNS configuration from "dns" action rules.
	DNSServers []option.DNSServerOptions
	DNSRules   []option.DNSRule
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
			outbound, tag := parseOutbound(r.ActionValue, "default-out")
			if outbound != nil {
				result.Outbounds = append(result.Outbounds, *outbound)
				result.Final = tag
				slog.Info("route rule: default outbound", "id", r.ID, "tag", tag)
			}

		case "dns":
			if r.ActionValue == "" || len(r.Match) == 0 {
				continue
			}
			serverTag := fmt.Sprintf("dns-%d", r.ID)
			result.DNSServers = append(result.DNSServers, option.DNSServerOptions{
				Type: C.DNSTypeUDP,
				Tag:  serverTag,
				Options: &option.RemoteDNSServerOptions{
					DNSServerAddressOptions: option.DNSServerAddressOptions{
						Server: r.ActionValue,
					},
				},
			})
			result.DNSRules = append(result.DNSRules, option.DNSRule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultDNSRule{
					RawDefaultDNSRule: option.RawDefaultDNSRule{
						DomainSuffix: badoption.Listable[string](r.Match),
					},
					DNSRuleAction: option.DNSRuleAction{
						Action: C.RuleActionTypeRoute,
						RouteOptions: option.DNSRouteActionOptions{
							Server: serverTag,
						},
					},
				},
			})
			slog.Info("route rule: dns", "id", r.ID, "server", r.ActionValue, "domains", len(r.Match))

		case "block_port":
			if len(r.Match) == 0 {
				continue
			}
			var ports badoption.Listable[uint16]
			var portRanges badoption.Listable[string]
			for _, m := range r.Match {
				if strings.Contains(m, "-") {
					portRanges = append(portRanges, m)
				} else {
					p, err := strconv.ParseUint(m, 10, 16)
					if err != nil {
						slog.Warn("block_port: invalid port number", "port", m, "id", r.ID)
						continue
					}
					ports = append(ports, uint16(p))
				}
			}
			if len(ports) > 0 || len(portRanges) > 0 {
				result.Rules = append(result.Rules, option.Rule{
					Type: C.RuleTypeDefault,
					DefaultOptions: option.DefaultRule{
						RawDefaultRule: option.RawDefaultRule{
							Port:      ports,
							PortRange: portRanges,
						},
						RuleAction: blockAction(),
					},
				})
				slog.Info("route rule: block ports", "id", r.ID, "ports", len(ports), "ranges", len(portRanges))
			}

		case "route":
			if len(r.Match) == 0 || r.ActionValue == "" {
				continue
			}
			fallbackTag := fmt.Sprintf("route-%d", r.ID)
			outbound, tag := parseOutbound(r.ActionValue, fallbackTag)
			if outbound == nil {
				continue
			}
			result.Outbounds = append(result.Outbounds, *outbound)
			result.Rules = append(result.Rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						DomainSuffix: badoption.Listable[string](r.Match),
					},
					RuleAction: option.RuleAction{
						Action: C.RuleActionTypeRoute,
						RouteOptions: option.RouteActionOptions{
							Outbound: tag,
						},
					},
				},
			})
			slog.Info("route rule: route domains", "id", r.ID, "tag", tag, "domains", len(r.Match))

		case "route_ip":
			if len(r.Match) == 0 || r.ActionValue == "" {
				continue
			}
			fallbackTag := fmt.Sprintf("route-ip-%d", r.ID)
			outbound, tag := parseOutbound(r.ActionValue, fallbackTag)
			if outbound == nil {
				continue
			}
			result.Outbounds = append(result.Outbounds, *outbound)
			for _, m := range r.Match {
				if strings.HasPrefix(m, "geoip:") {
					code := strings.TrimPrefix(m, "geoip:")
					rsTag := "geoip-" + code
					result.RuleSets = append(result.RuleSets, option.RuleSet{
						Type:   C.RuleSetTypeRemote,
						Tag:    rsTag,
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
								RuleSet: badoption.Listable[string]{rsTag},
							},
							RuleAction: option.RuleAction{
								Action: C.RuleActionTypeRoute,
								RouteOptions: option.RouteActionOptions{
									Outbound: tag,
								},
							},
						},
					})
					slog.Info("route rule: route geoip via rule_set", "id", r.ID, "code", code, "tag", tag)
				} else {
					result.Rules = append(result.Rules, option.Rule{
						Type: C.RuleTypeDefault,
						DefaultOptions: option.DefaultRule{
							RawDefaultRule: option.RawDefaultRule{
								IPCIDR: badoption.Listable[string]{m},
							},
							RuleAction: option.RuleAction{
								Action: C.RuleActionTypeRoute,
								RouteOptions: option.RouteActionOptions{
									Outbound: tag,
								},
							},
						},
					})
					slog.Info("route rule: route ip cidr", "id", r.ID, "cidr", m, "tag", tag)
				}
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

// BuildSourceIPRules creates route rules and rule_sets that block
// incoming connections from the specified source IPs/GeoIPs.
// Supports "geoip:XX" format and plain IP CIDRs.
func BuildSourceIPRules(sources []string) (rules []option.Rule, ruleSets []option.RuleSet) {
	for _, s := range sources {
		if strings.HasPrefix(s, "geoip:") {
			code := strings.TrimPrefix(s, "geoip:")
			tag := "geoip-" + code
			ruleSets = append(ruleSets, option.RuleSet{
				Type:   C.RuleSetTypeRemote,
				Tag:    tag,
				Format: C.RuleSetFormatBinary,
				RemoteOptions: option.RemoteRuleSet{
					URL:            "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-" + code + ".srs",
					DownloadDetour: "direct",
				},
			})
			rules = append(rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						RuleSet:                  badoption.Listable[string]{tag},
						RuleSetIPCIDRMatchSource: true,
					},
					RuleAction: blockAction(),
				},
			})
			slog.Info("source ip rule: block geoip", "code", code)
		} else {
			rules = append(rules, option.Rule{
				Type: C.RuleTypeDefault,
				DefaultOptions: option.DefaultRule{
					RawDefaultRule: option.RawDefaultRule{
						SourceIPCIDR: badoption.Listable[string]{s},
					},
					RuleAction: blockAction(),
				},
			})
			slog.Info("source ip rule: block cidr", "cidr", s)
		}
	}
	return
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

func parseOutbound(actionValue, fallbackTag string) (*option.Outbound, string) {
	if actionValue == "" {
		return nil, ""
	}

	var cfg v2rayOutConfig
	if err := json.Unmarshal([]byte(actionValue), &cfg); err != nil {
		slog.Error("failed to parse outbound config", "error", err)
		return nil, ""
	}

	tag := cfg.Tag
	if tag == "" {
		tag = fallbackTag
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
