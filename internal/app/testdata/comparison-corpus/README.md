# OpenBindings Comparison Conformance Corpus

Pilot fixtures for the comparison convention in `../../conventions/comparison.md`. These fixtures are **not** part of core OpenBindings spec conformance; they lock behavior for the official comparison tooling profile `OB-2020-12`.

## Layout

```
conformance/comparison/
  README.md
  manifest.json
  matching/
    *.json
  modes/
    *.json
  profile/
    *.json
  structural/
    *.json
  subsumption/
    *.json
  suppression/
    *.json
```

Each fixture is a single `ob-comparison-fixture/v1` JSON file validated by `conventions/schemas/fixture.schema.json`.

## Pilot Coverage

The first tranche covers high-risk direction-sensitive and undecidable cases:

- required properties across input/output variance
- additionalProperties input rejection
- number/integer narrowing across input/output variance
- numeric bound tightening in output
- enum value addition in output
- regex containment as `unverified`
- profile-conformance error as `indeterminate`
- operation add/remove structural deltas
- summary verdict collapse precedence
- alias pairing
- identical mode normalization
- suppression audit output

This corpus should be expanded after one implementation can produce byte-equivalent reports for these pilot fixtures.
