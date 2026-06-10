# Data Layer and Dataset Recommendation

Use this reference whenever the task needs dataset choice, band semantics, numeric transformations, QA masks, or storage scaling.

## Data Semantics Gate

Before final code depends on a dataset, identify:

- target variable and physical meaning
- dataset ID and whether it is official or community-hosted
- bands or fields used
- units
- raw value range
- valid physical range
- scale factor and offset
- nodata, fill, mask, or QA rules
- projection, nominal scale, and CRS if relevant
- dtype before and after transformation

If any critical item is unknown, write `UNKNOWN` in `DATA_LAYER.md` and verify from documentation or ask the user.

## Dataset Recommendation Standard

When recommending data, create a shortlist before writing final code.

1. Clarify target variable, region, time range, resolution, official-vs-community preference, license constraints, and output goal.
2. Search official Earth Engine docs/catalog for official assets.
3. Search `awesome-gee-community-datasets/community_datasets.csv` for community assets.
4. Mark each candidate as `Official` or `Community`.
5. Check license and docs page before recommending a community dataset.
6. Do not infer band schemas, units, scale factor, offset, or QA rules from the CSV alone.

## Community CSV Fields

Use a CSV parser and search across:

- `title`
- `tags`
- `thematic_group`
- `provider`
- `type`

Return:

- `id`
- `provider`
- `title`
- `type`
- `tags`
- `sample_code`
- `license`
- `docs_page`

## PowerShell Lookup Example

```powershell
$rows = Import-Csv .\awesome-gee-community-datasets\community_datasets.csv
$rows |
  Where-Object {
    $_.title -match "precipitation|rainfall" -or
    $_.tags -match "precipitation|rainfall" -or
    $_.thematic_group -match "climate|weather"
  } |
  Select-Object -First 10 id, provider, title, type, tags, license, docs_page
```

## Transformation Rules

- Keep physical meaning separate from storage format.
- Record every formula and expected output range.
- If scaling to integer storage, state how to restore physical values.
- Use `clamp` only when truncation is scientifically acceptable.
- Preserve useful metadata with `copyProperties` when mapping over images.
- Do not silently mix reflectance, scaled reflectance, temperature, index, categorical, and QA bands.
