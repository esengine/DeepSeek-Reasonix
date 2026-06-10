# Sources

- GAUL level1 usage pattern: `google-earth-engine/tutorials/community/data-converters.md`
- Landsat Collection 2 scaling example: `google-earth-engine/guides/arrays_transformations.md`
- dNBR and NBR references: `google-earth-engine/api_docs.md`
- Boundary and compute guidance: `references/boundary_compute.md`
- Data layer guidance: `references/data_layer.md`

## Key Evidence

- ROI uses `FAO/GAUL/2015/level1` with `ADM1_NAME = "New South Wales"`.
- Landsat C2 L2 optical scale/offset: `* 0.0000275 - 0.2`.
- NBR uses NIR and SWIR2; for Landsat 8/9 these are `SR_B5` and `SR_B7`.
- dNBR is `pre_NBR - post_NBR`.
