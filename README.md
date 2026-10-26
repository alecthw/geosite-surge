# geosite-surge

Convert `geosite.dat` into Surge `.list` rule files.

## Usage

```bash
go run . -codes steam -out surge-rules
```

When `geosite.dat` does not exist, the tool downloads it from:

```text
https://github.com/Loyalsoldier/v2ray-rules-dat/raw/refs/heads/release/geosite.dat
```

By default, all geosite codes are exported:

```bash
go run . -out surge-rules
```

To export selected codes:

```bash
go run . -codes steam,google,geolocation-!cn -out surge-rules
```

Example codes that exercise all Surge rule types and attribute outputs:

```bash
go run . -codes google,steam,epicgames,private,ccb,category-ads -out surge-rules
```

Attribute-tagged domains are written to separate files. For example, exporting
`steam` can create both:

```text
surge-rules/steam.list
surge-rules/steam@cn.list
```

`include:` references are resolved recursively before writing the Surge rules.
Include filters are supported: `include:listb @attr1 @-attr2` includes entries
from `listb` that have `attr1` and do not have `attr2`.

## Rule Mapping

| geosite type | Surge rule |
| --- | --- |
| `Plain` | `DOMAIN-KEYWORD` |
| `RootDomain` | `DOMAIN-SUFFIX` |
| `Full` | `DOMAIN` |
| `Regex` | see below |

geosite regexes match the hostname, while Surge `URL-REGEX` matches the full
request URL, so regexes are converted to domain rules instead:

- Finite anchored patterns expand to `DOMAIN`/`DOMAIN-SUFFIX` (up to 200 rules),
  which Surge matches through its domain index.
- Larger patterns become `DOMAIN-WILDCARD` rules. Character classes are written
  as enumerated `[...]` classes, `.` becomes `?`, and `(^|\.)x` produces both
  `x` and `*.x` because `*.x` does not match `x` itself.
- Unbounded class repeats such as `[0-9]+` become `[0-9]*`, which also matches
  other characters; such approximations are kept only when the wildcard has at
  least 3 literal characters outside the last label.
- Patterns that cannot be expressed, such as lookarounds or single-label
  hostname patterns, are skipped with a warning on stderr.

`DOMAIN` and `DOMAIN-SUFFIX` rules already covered by a `DOMAIN-SUFFIX` rule in
the same file are removed.
