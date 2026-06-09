# geosite-to-surge-rule

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
| `Regex` | safely converted to `DOMAIN`, `DOMAIN-SUFFIX`, or `DOMAIN-WILDCARD`; otherwise `URL-REGEX` |
