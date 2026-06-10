# Local Python and Earth Engine Environment Gate

Use this reference before assuming a generated GEE/geemap script can run on the
current machine. This is the first gate for GEEMu: do not start business logic
until geemap, Earth Engine authentication, and the Cloud Project are configured.

## What to Check

Check the local environment before final code when:

- the user wants runnable Python
- the task uses geemap
- the task uses a local shapefile, GeoJSON, GeoPackage, or GeoDataFrame
- the task needs Earth Engine authentication
- the user says the machine is already authenticated

Run:

```powershell
python scripts\check_local_environment.py --skip-ee-network
```

If runnable GEE/geemap code is requested, require a Cloud Project before doing
online work:

```powershell
python scripts\check_local_environment.py --skip-ee-network --require-project
```

For an online authentication check:

```powershell
python scripts\check_local_environment.py --project PROJECT_ID --require-project --require-auth
```

Use `--skip-ee-network` when you only want to check imports and credential files.
Use `--project` when verifying Earth Engine API access.

## Required Packages

- `earthengine-api`, imported as `ee`
- `geemap`
- `geopandas`, optional but recommended for local vector workflows

## Authentication Logic

If a credential file exists, the machine is likely authenticated locally, but a
real API call is still the stronger check. On Windows, the common credential path
is:

```text
%USERPROFILE%\.config\earthengine\credentials
```

Rules:

- If `ee` is missing, do not write code that assumes Earth Engine can run.
- If `geemap` is missing, guide the user to install it before using this skill's
  Python/geemap workflow.
- If the Cloud Project ID is missing, stop and ask the user to provide it or set
  `EE_PROJECT`, `GOOGLE_CLOUD_PROJECT`, or `EE_PROJECT_ID`.
- If credentials exist but `geemap.ee_initialize(project=...)` or
  `ee.Initialize(project=...)` fails, ask about Cloud Project,
  proxy, account permissions, or expired credentials.
- If the user has already authenticated locally, do not force `ee.Authenticate()`;
  use a try/except pattern that only authenticates when initialization fails.

## Preferred Initialization Pattern

```python
import ee
import geemap

PROJECT_ID = "PROJECT_ID"  # Ask the user for this before online work.

if PROJECT_ID == "PROJECT_ID":
    raise RuntimeError("Cloud Project ID is required before Earth Engine requests.")

geemap.ee_initialize(project=PROJECT_ID)
ee.data.getAssetRoots()
```

Use `geemap.ee_initialize(project=PROJECT_ID)` as the preferred path because it
handles common local authentication flows cleanly. Keep `PROJECT_ID` explicit for
portability and newer Earth Engine workflows even if one machine can initialize
without it.

## Run Log Fields

Record in `RUN.md`:

- Python executable
- Python version
- `ee` import status
- `geemap` import status
- `geopandas` import status if local vector data is used
- credential file found: yes/no
- online Earth Engine check: skipped/passed/failed
- Cloud Project used
