# Example: NSW dNBR Burned Area and NBR Recovery

This GEEMu example uses New South Wales, Australia as the ROI.

The workflow first detects burned area from dNBR, then analyzes recovery only
inside the detected burned-area mask using NBR.

## What It Does

- ROI: `FAO/GAUL/2015/level1`, `ADM1_NAME = "New South Wales"`
- resolution: `SCALE = 100`
- pre-fire NBR: 2018-10-01 to 2019-03-31
- post-fire NBR: 2020-02-01 to 2020-05-31
- recovery NBR: 2023-10-01 to 2024-03-31
- burned area: `dNBR >= 0.20`
- recovery: `(recovery_NBR - post_NBR) / dNBR`

## Dry Run

```powershell
python examples\nsw_dnbr_recovery\code.py --dry-run
```

## Actual Summary Run

Requires a valid Earth Engine Cloud Project:

```powershell
python examples\nsw_dnbr_recovery\code.py --project PROJECT_ID --no-map
```

## Optional Export

```powershell
python examples\nsw_dnbr_recovery\code.py --project PROJECT_ID --export --no-map
```

The default actual run computes a summary first. Exports are opt-in because NSW
is large even at 100 m.
