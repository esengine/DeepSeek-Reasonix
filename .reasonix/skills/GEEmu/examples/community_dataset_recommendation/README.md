# Example: Community Dataset Recommendation

This example shows how GEEMu should use
`awesome-gee-community-datasets/community_datasets.csv` as a structured lookup
table for community dataset suggestions.

It demonstrates:

- searching by target keywords
- scoring `title`, `tags`, `thematic_group`, `provider`, and `type`
- reporting dataset ID, provider, license, sample code, and docs page
- marking results as candidate recommendations, not verified final code inputs

## Run

```powershell
python examples\community_dataset_recommendation\code.py
```

The script writes `community_candidates.csv` in this example folder.
