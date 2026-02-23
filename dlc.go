// DLC (dlc.dat) parsing using protobuf-generated GeoSiteList.
// Minimal proto copied from v2fly/v2ray-core routercommon; no protoext to avoid
// extension 50000 conflict with grpc.

package ruledforward

import (
	"errors"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/hr3lxphr6j/coredns-ruledforward/internal/dlcpb"
)

var errInvalidDLC = errors.New("invalid dlc.dat: not a valid GeoSiteList protobuf")

// LoadDLC reads a dlc.dat file and returns a map from list name (country_code) to rules.
// List names are normalized to uppercase (e.g. "google", "cn").
// For geosite:list@attr filtering, rules with attributes are also keyed by "LIST@ATTR"
// (e.g. "GOOGLE@ADS"). Use geosite google@ads in config to get only domains with @ads.
// Uses a minimal GeoSiteList proto (see proto/geosite.proto) to avoid importing
// v2fly/v2ray-core and its proto extension 50000 conflict with grpc.
func LoadDLC(path string) (map[string][]Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadDLCWire(data)
}

// loadDLCWire unmarshals dlc.dat bytes (GeoSiteList protobuf) and returns
// a map from list name to rules.
func loadDLCWire(data []byte) (map[string][]Rule, error) {
	if len(data) == 0 {
		return nil, errInvalidDLC
	}
	var list dlcpb.GeoSiteList
	if err := proto.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make(map[string][]Rule)
	for _, entry := range list.GetEntry() {
		name := entry.GetCountryCode()
		if name == "" {
			name = strings.ToUpper(entry.GetCode())
		}
		if name == "" {
			continue
		}
		name = strings.ToUpper(name)
		for _, d := range entry.GetDomain() {
			r, ok := domainToRule(d)
			if !ok {
				continue
			}
			out[name] = append(out[name], r)
			for _, a := range d.GetAttribute() {
				if a != nil && a.GetKey() != "" {
					attrKey := name + "@" + strings.ToUpper(a.GetKey())
					out[attrKey] = append(out[attrKey], r)
				}
			}
		}
	}
	if len(out) == 0 && len(data) > 0 {
		return nil, errInvalidDLC
	}
	return out, nil
}

func domainToRule(d *dlcpb.Domain) (Rule, bool) {
	if d == nil {
		return Rule{}, false
	}
	val := strings.ToLower(strings.TrimSpace(d.GetValue()))
	if val == "" {
		return Rule{}, false
	}
	var r Rule
	switch d.GetType() {
	case dlcpb.Domain_RootDomain:
		r = Rule{Type: RuleDomain, Value: val}
	case dlcpb.Domain_Full:
		r = Rule{Type: RuleFull, Value: val}
	case dlcpb.Domain_Regex:
		r = Rule{Type: RuleRegex, Value: val}
	case dlcpb.Domain_Plain:
		r = Rule{Type: RuleKeyword, Value: val}
	default:
		return Rule{}, false
	}
	return r, true
}
