# ruledforward

## Name

*ruledforward* - forwards DNS queries or returns empty (NODATA) by matching domain rules. Supports v2fly
domain-list-community (dlc.dat), AdGuard Home filter rules, and inline domain-list-community-style rules.

## Description

*ruledforward* matches the query name against ordered **groups**. Each group has:

- Rules from **dlc.dat** (geosite list names), **AdGuard rules** (local file or URL), and/or **inline rules** (
  `domain:`, `full:`, etc.).
- An **action**: **forward** (resolve via the group's upstreams) or **empty** (return NODATA for DNS filtering).

The first group whose rules match the qname is used. Within each group, a Bloom filter is used to quickly skip
non-matching queries before full rule matching.

## External plugin

This is a **CoreDNS external plugin**. To use it:

1. In your CoreDNS repo's **plugin.cfg**, add (or ensure you have):
   ```
   ruledforward:github.com/hr3lxphr6j/coredns-ruledforward
   ```
2. In CoreDNS **go.mod**, add:
   ```
   require github.com/hr3lxphr6j/coredns-ruledforward v0.0.0
   replace github.com/hr3lxphr6j/coredns-ruledforward => ../coredns-ruledforward
   ```
   (Adjust the `replace` path if your plugin repo is elsewhere.)
3. Run `go generate && go build` in the CoreDNS directory.

## Syntax

~~~
ruledforward [FROM] {
    dlcfile PATH
    group NAME {
        action empty|forward
        geosite LIST...
        domain: DOMAIN
        full: DOMAIN
        adguard_rules PATH|URL...
        refresh CRON
        to TO...
        policy random|round_robin|sequential
        # optional: max_fails, tls, expire, force_tcp, prefer_udp, etc.
    }
}
~~~

- **FROM** – Zone to match (default: `.`). Only queries in this zone are handled.
- **dlcfile** – Path to a local **dlc.dat** file (v2fly domain-list-community build). Required if any group uses *
  *geosite**.
- **group** – Defines one rule group (order matters; first match wins).
    - **action** – `empty`: return NODATA (no upstream). `forward`: resolve via **to** (default).
    - **geosite** – List names from dlc.dat (e.g. `google`, `cn`, `category-ads-all`). Use **geosite:list@attr** to
      include only domains that have that attribute in the list (e.g. `geosite google@ads` for ad-related domains only).
    - **domain:** / **full:** – Inline domain-list-community-style rules (no `include:`).
    - **adguard_rules** – Paths or `https://`/`http://` URLs to AdGuard-style filter files.
    - **refresh** – Cron expression (e.g. `0 */6 * * *`) to periodically re-fetch **adguard_rules** URLs and update the
      group.
    - **to** – Upstream addresses (only for **action forward**). Same syntax as the *forward* plugin (`tls://`, etc.).
    - **policy** – Load-balance policy: `random`, `round_robin`, or `sequential`.

## Examples

Return empty for ad/tracking domains (using list + attribute), forward the rest to 8.8.8.8:

~~~
ruledforward . {
    dlcfile /etc/coredns/dlc.dat
    group block_ads {
        action empty
        geosite category-ads-all google@ads
    }
    group default {
        action forward
        to 8.8.8.8
        policy round_robin
    }
}
~~~

Use AdGuard list from the network and refresh every 6 hours:

~~~
ruledforward . {
    dlcfile /etc/coredns/dlc.dat
    group block {
        action empty
        geosite category-ads-all
        adguard_rules https://raw.githubusercontent.com/.../list.txt
        refresh "0 */6 * * *"
    }
    group default {
        action forward
        to 1.1.1.1 8.8.8.8
        policy sequential
    }
}
~~~

With *cache* (cache then rule-based forward):

~~~
. {
    cache 30
    ruledforward . {
        dlcfile /etc/coredns/dlc.dat
        group direct {
            action forward
            geosite cn private
            to 119.29.29.29
            policy sequential
        }
        group proxy {
            action forward
            geosite google
            to 8.8.8.8
            policy round_robin
        }
    }
    forward . 8.8.8.8
}
~~~

## dlc.dat

Download from [v2fly/domain-list-community Releases](https://github.com/v2fly/domain-list-community/releases) (e.g. *
*dlc.dat**), or build from source. **ruledforward** does not use the repo's `data/` directory directly.

## AdGuard rules

- **Paths** – Read at startup; no automatic refresh.
- **URLs** – Fetched at startup; if the group has **refresh** (cron), URLs are re-fetched on that schedule and the
  group's rules are updated.

Supported formats: domains-only, `||domain^`, `/regex/`, hosts-style lines; `#`/`!` comments and `@@` exceptions are
ignored.

## Metrics

If the *prometheus* plugin is enabled, *ruledforward* exposes:

- **coredns_ruledforward_requests_total** – Counter of requests per group and action (`group`, `action` where action is
  `empty` or `forward`).
- **coredns_ruledforward_no_match_total** – Counter of requests that did not match any group (passed to next plugin).
- **coredns_ruledforward_forward_upstream_fail_total** – Counter of forward requests where all upstreams failed (`group`
  label).

## Compatibility

- Placed before *forward* in the plugin chain so it can route by rule first; remaining queries fall through to
  *forward*.
- Works with *cache*: unmatched queries are passed to the next plugin; matched ones are answered by *ruledforward* (
  forward or empty).

## Development

- **Proto codegen**: dlc.dat is parsed via a minimal GeoSiteList protobuf (see `proto/geosite.proto`). After editing the proto, run `make generate` (requires `protoc` and `protoc-gen-go`).
- **Matcher concurrency**: Matcher has no internal lock; the holder (Group) uses `atomic.Pointer` + `Store`/`Load` for concurrent safety. On refresh, a new matcher is built and atomically swapped via `SetMatcher`.

## Also see

- [v2fly/domain-list-community](https://github.com/v2fly/domain-list-community)
- [AdGuard DNS filtering syntax](https://adguard-dns.io/kb/general/dns-filtering-syntax/)
- *forward* plugin for upstream options (tls, max_fails, policy, etc.)
