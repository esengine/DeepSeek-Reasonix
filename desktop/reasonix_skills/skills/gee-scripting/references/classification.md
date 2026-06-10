# Classification and Clustering

## 1. Classifier Selection

GEE's supervised classification is performed via `ee.Classifier`. There is no single officially designated best classifier, but based on the number of examples, output mode support, and asset export capability, **Random Forest is the most suitable default starting point**.

### Common Classifier Comparison

| Classifier | Constructor | Characteristics | Use Case |
|------------|-------------|-----------------|----------|
| **Random Forest** | `smileRandomForest` | Robust, supports all output modes, exportable as asset | **Default first choice**, land cover, crop identification, etc. |
| CART | `smileCart` | Single decision tree, clear and interpretable structure | Teaching, quick baseline, when split logic needs explanation |
| Gradient Boosted Trees | `smileGradientTreeBoost` | Boosted ensemble, potentially stronger than RF | Compare with RF when higher accuracy is needed |
| SVM | `libsvm` | Flexible kernel functions, most parameters | Small sample sizes, willingness to tune parameters |
| KNN | `smileKNN` | Simple nearest-neighbor classification | Quick control experiments, low-dimensional feature spaces |
| Naive Bayes | `smileNaiveBayes` | Few parameters, **requires non-negative integer features** | Discrete/count features, not suitable for continuous reflectance |
| Minimum Distance | `minimumDistance` | Based on distance to class centroids | Quick baseline for traditional spectral classification |
| MaxEnt | `amnhMaxent` | Probability modeling based on presence points | Species distribution modeling, not suitable for general land classification |

### Random Forest Parameters

```python
classifier = ee.Classifier.smileRandomForest(
    numberOfTrees=200,       # Number of trees; more = more stable but slower
    variablesPerSplit=None,  # Features per split; default sqrt(total features)
    minLeafPopulation=1,     # Minimum samples per leaf node
    bagFraction=0.5,         # Sample fraction per tree
    maxNodes=None,           # Maximum nodes; None = unlimited
    seed=42
)
```

### Output Modes

Use `setOutputMode()` to set the classifier output type:

| Mode | Description |
|------|-------------|
| `CLASSIFICATION` | Default, outputs class labels |
| `PROBABILITY` | Outputs maximum probability value |
| `MULTIPROBABILITY` | Outputs probability array for each class |
| `RAW` | Outputs raw vote/decision values |

Different classifiers support different output modes; Random Forest has the most complete support.

---

## 2. Training Samples

Constructing training samples (where to label which classes) is the user's domain expertise — the Agent cannot substitute for this. The Agent's responsibility is to **guide the user in preparing training data that meets requirements, then use that data to complete the classification**.

### Guiding the User to Prepare Training Data

Before starting classification, confirm the following with the user:

| Item to Confirm | Description |
|-----------------|-------------|
| **Sample file format** | .shp / .geojson / .gpkg vector files, or already-classified raster images |
| **Class field name** | Which attribute field stores the class labels (e.g., `landcover`, `class`, `label`) |
| **Classification scheme** | What classes exist, what numbers represent each |
| **Sample distribution** | Whether all major classes and geographic regions within the study area are covered |

If the user doesn't have training samples yet, suggest:
- Manually digitize points or polygons in QGIS, ArcGIS, or GEE Code Editor using high-resolution basemaps
- Label at least 30–50 sample points per class, distributed across the study area
- Use **consecutive integers starting from 0** for label encoding (0, 1, 2, 3...), otherwise the confusion matrix will have empty rows and columns
- If original encoding is non-consecutive (e.g., 10, 20, 30...), use `remap()` in the script to convert

### Two Sampling Methods

Choose based on the user's data format:

| User's Data | Sampling Method | Description |
|-------------|----------------|-------------|
| **Vector sample file** (points/polygons + class attributes) | `sampleRegions()` | Most common scenario; extracts image band values at sample locations |
| **Already-classified raster image** (e.g., WorldCover) | `sample()` | Stack label image with feature image, then randomly extract pixels |

#### sampleRegions(): Using User's Vector Samples

```python
# Load user-provided training samples
training_fc = load_region("/path/to/samples.shp")

with heartbeat("Sampling from image"):
    training = composite.sampleRegions(
        collection=training_fc,
        properties=["landcover"],  # Class field name in user's samples
        scale=10,
        tileScale=4,       # Increase to mitigate memory issues (2/4/8/16)
        geometries=False    # Don't preserve geometry, saves memory
    )
    sample_count = training.size().getInfo()
print(f"Training samples: {sample_count}")
```

**Note:** If all input features are points, `reduceRegions()` is more memory-efficient than `sampleRegions()`.

#### sample(): Using Existing Classified Image as Labels

```python
# Stack label image with feature image
label_img = ee.Image("ESA/WorldCover/v100/2020").remap(
    [10, 20, 30, 40, 50, 60, 70, 80, 90, 95, 100],
    ee.List.sequence(0, 10)
).rename("lc").toByte()

stack = composite.addBands(label_img)

with heartbeat("Random sampling"):
    samples = stack.sample(
        region=roi,
        scale=10,
        numPixels=5000,     # Target sample count (approximate)
        seed=42,
        dropNulls=True,     # Default True, removes samples containing nulls
        geometries=True,    # Preserve geometry; needed for spatial split validation
        tileScale=4
    )
```

### Sample Size Limits

GEE has no hard limit on sample count, but total training data size should be kept under approximately **100 MB**. Rough estimate (32-bit float): `samples × bands ≤ (100 × 2^20) / 4`, e.g., with 100 bands, samples should be fewer than ~200,000.

Interactive training exceeding **99 MB** or **5 minutes** may fail — in that case, switch to `Export.classifier.toAsset` for batch processing. Classifiers that support export: `smileRandomForest`, `smileCart`, `DecisionTree`, `DecisionTreeEnsemble`.

---

## 3. Train/Validation Split

### Standard Approach: randomColumn() + filter()

```python
samples = samples.randomColumn(columnName="random", seed=42)

split = 0.7
training = samples.filter(ee.Filter.lt("random", split))
validation = samples.filter(ee.Filter.gte("random", split))
```

`randomColumn()` adds a column of uniformly distributed pseudo-random numbers in [0, 1) to the FeatureCollection. Specifying `seed` ensures reproducibility.

### Spatial Separation Validation (More Rigorous)

Random splitting does not guarantee spatial independence — adjacent pixels often have spatial autocorrelation, leading to inflated validation accuracy. The official approach provides a spatial filtering solution:

```python
# geometries=True is required during sampling
samples = stack.sample(
    region=roi, scale=10, numPixels=5000,
    seed=42, geometries=True, tileScale=16
).randomColumn()

training = samples.filter(ee.Filter.lt("random", 0.7))
validation = samples.filter(ee.Filter.gte("random", 0.7))

# Remove training samples too close to validation samples
dist_filter = ee.Filter.withinDistance(
    distance=1000,       # In meters
    leftField=".geo",
    rightField=".geo",
    maxError=10
)
training = ee.FeatureCollection(
    ee.Join.inverted().apply(training, validation, dist_filter)
)
```

Spatial separation validation requires a larger `tileScale` (e.g., 16) to avoid memory issues.

---

## 4. Accuracy Assessment

### Two Core Interfaces

| Interface | Purpose | Data Source |
|-----------|---------|-------------|
| `classifier.confusionMatrix()` | Training set confusion matrix (resubstitution error) | Training data |
| `featureCollection.errorMatrix(actual, predicted)` | Validation set error matrix | Classified validation set |

Training set accuracy is typically inflated (the model has already fitted the training samples) — **validation set accuracy should be the standard**.

### Accuracy Metrics

| Metric | Method | Meaning |
|--------|--------|---------|
| Overall Accuracy (OA) | `.accuracy()` | Proportion correctly classified |
| Kappa Coefficient | `.kappa()` | Accuracy after excluding chance agreement |
| User's Accuracy (UA) | `.consumersAccuracy()` | Computed by row; of samples predicted as this class, how many are correct |
| Producer's Accuracy (PA) | `.producersAccuracy()` | Computed by column; of actual samples of this class, how many were correctly identified |

### Complete Accuracy Assessment Workflow

```python
# Training
with heartbeat("Training classifier"):
    classifier = ee.Classifier.smileRandomForest(
        numberOfTrees=200, seed=42
    ).train(
        features=training,
        classProperty="lc",
        inputProperties=composite.bandNames()
    )

# Training set accuracy (resubstitution error)
with heartbeat("Evaluating training accuracy"):
    train_cm = classifier.confusionMatrix()
    train_oa = train_cm.accuracy().getInfo()
print(f"Training OA: {train_oa:.4f}")

# Validation set classification and accuracy
with heartbeat("Evaluating validation accuracy"):
    validated = validation.classify(classifier)
    val_cm = validated.errorMatrix("lc", "classification")
    val_oa = val_cm.accuracy().getInfo()
    val_kappa = val_cm.kappa().getInfo()
    val_ua = val_cm.consumersAccuracy().getInfo()
    val_pa = val_cm.producersAccuracy().getInfo()

print(f"Validation OA: {val_oa:.4f}")
print(f"Validation Kappa: {val_kappa:.4f}")
print(f"User Accuracy: {val_ua}")
print(f"Producer Accuracy: {val_pa}")
```

### Common Pitfalls

When class encodings are not consecutive integers starting from 0, `confusionMatrix()` and `errorMatrix()` will produce empty rows and columns. Always use `remap()` to convert labels before training.

---

## 5. Unsupervised Classification (Clustering)

GEE clustering is performed via `ee.Clusterer`, based on Weka. Unlike supervised classification, the training set has no class labels, and the output is just **integer cluster IDs** that require subsequent manual semantic assignment.

### Common Clustering Algorithms

| Algorithm | Constructor | Characteristics |
|-----------|-------------|-----------------|
| **KMeans** | `wekaKMeans` | Most common; requires specifying number of clusters |
| XMeans | `wekaXMeans` | Automatically estimates cluster count (within a given range) |
| CascadeKMeans | `wekaCascadeKMeans` | Selects optimal k by Calinski-Harabasz criterion |
| LVQ | `wekaLVQ` | Learning Vector Quantization |
| Cobweb | `wekaCobweb` | Conceptual clustering, **may be very slow and generate many clusters** |

### Clustering Workflow

```python
# Random sampling (no labels needed)
with heartbeat("Sampling for clustering"):
    cluster_training = composite.sample(
        region=roi, scale=10, numPixels=5000, seed=42
    )

# Training
with heartbeat("Training clusterer"):
    clusterer = ee.Clusterer.wekaKMeans(nClusters=8).train(cluster_training)

# Apply to image
with heartbeat("Clustering"):
    clustered = composite.cluster(clusterer)

download_image(clustered, "/path/to/clustered.tif", roi, scale=10, dtype="uint8")
```

### When Cluster Count is Unknown

```python
# XMeans automatically selects optimal cluster count between 4-12
clusterer = ee.Clusterer.wekaXMeans(
    minClusters=4, maxClusters=12, seed=42
).train(cluster_training)
```

### Assigning Semantics to Clusters

The clusterer only outputs integer IDs — it won't tell you "cluster 3 is forest." Assignment methods:

1. **Compute mean spectral/index values per cluster** — use `reduceRegion` + `Reducer.mean().group()` for grouped statistics
2. **Cross-reference with existing land cover maps** — overlay with existing classification products like WorldCover
3. **Manual interpretation** — combine with high-resolution basemaps

```python
# Compute mean band values per cluster
cluster_stack = composite.addBands(clustered.rename("cluster"))

with heartbeat("Computing cluster statistics"):
    stats = cluster_stack.reduceRegion(
        reducer=ee.Reducer.mean().group(
            groupField=cluster_stack.bandNames().size().subtract(1),
            groupName="cluster"
        ),
        geometry=roi, scale=10, maxPixels=1e9
    ).getInfo()

for group in stats["groups"]:
    print(f"Cluster {group['cluster']}: {group}")
```

---

## 6. Feature Engineering

There is no fixed official feature combination recommendation, but the API supports constructing various derived features to improve classification accuracy.

### Spectral Indices

```python
ndvi = composite.normalizedDifference(["B8", "B4"]).rename("NDVI")
ndwi = composite.normalizedDifference(["B3", "B8"]).rename("NDWI")
ndbi = composite.normalizedDifference(["B11", "B8"]).rename("NDBI")
```

**Note:** `normalizedDifference()` will mask the output when either input band has negative values. To avoid this, use `expression()` instead:

```python
ndvi = composite.expression(
    "(nir - red) / (nir + red)",
    {"nir": composite.select("B8"), "red": composite.select("B4")}
).rename("NDVI")
```

### DEM and Terrain

```python
dem = ee.Image("NASA/NASADEM_HGT/001").select("elevation")
terrain = ee.Terrain.products(dem)  # Computes slope, aspect, hillshade at once

slope = terrain.select("slope")
aspect = terrain.select("aspect")
```

`ee.Terrain.products()` input should be an elevation band in meters. Terrain features are particularly useful for vegetation classification in mountainous areas and landform identification.

### GLCM Texture

`glcmTexture()` computes texture metrics based on the Gray-Level Co-occurrence Matrix. **Input must be integer-type images.** Each input band produces 18 texture bands, so use selectively:

```python
# Select one band, quantize to 0-255 integer
nir_q = composite.select("B8").unitScale(0, 4000).multiply(255).toByte()

# Compute GLCM texture
texture = nir_q.glcmTexture(size=1)

# Select commonly used metrics
texture_select = texture.select(["B8_contrast", "B8_entropy", "B8_idm"])
```

It's recommended to compute texture on only 1–2 key bands, selecting representative metrics like contrast, entropy, and homogeneity (idm) to avoid feature dimension explosion.

### Combined Feature Stack

```python
predictors = composite.addBands([
    ndvi, ndwi, ndbi,
    dem.rename("DEM"), slope, aspect,
    texture_select
])
```

---

## Complete Workflow Example

```python
from geocode import init_gee, load_region, download_image, heartbeat
import ee

init_gee("project-id")
roi = load_region("/path/to/boundary.shp").geometry()

# 1. Build feature image
with heartbeat("Building composite"):
    composite = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2024-01-01", "2025-01-01")
        .filterBounds(roi)
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20))
        .median().clip(roi)
        .select(["B2", "B3", "B4", "B8", "B11", "B12"]))

# Add auxiliary features
ndvi = composite.normalizedDifference(["B8", "B4"]).rename("NDVI")
dem = ee.Image("NASA/NASADEM_HGT/001").select("elevation")
slope = ee.Terrain.slope(dem)
predictors = composite.addBands([ndvi, dem.rename("DEM"), slope])

# 2. Prepare training data
training_fc = load_region("/path/to/samples.shp")

with heartbeat("Sampling training data"):
    samples = predictors.sampleRegions(
        collection=training_fc,
        properties=["landcover"],
        scale=10,
        tileScale=4
    )
    total = samples.size().getInfo()
print(f"Total samples: {total}")

# 3. Train/validation split
samples = samples.randomColumn(seed=42)
training = samples.filter(ee.Filter.lt("random", 0.7))
validation = samples.filter(ee.Filter.gte("random", 0.7))

# 4. Train
with heartbeat("Training Random Forest"):
    classifier = ee.Classifier.smileRandomForest(
        numberOfTrees=200, seed=42
    ).train(
        features=training,
        classProperty="landcover",
        inputProperties=predictors.bandNames()
    )

# 5. Accuracy assessment
with heartbeat("Evaluating accuracy"):
    train_oa = classifier.confusionMatrix().accuracy().getInfo()

    validated = validation.classify(classifier)
    val_cm = validated.errorMatrix("landcover", "classification")
    val_oa = val_cm.accuracy().getInfo()
    val_kappa = val_cm.kappa().getInfo()

print(f"Training OA: {train_oa:.4f}")
print(f"Validation OA: {val_oa:.4f}")
print(f"Validation Kappa: {val_kappa:.4f}")

# 6. Classify and export
with heartbeat("Classifying"):
    classified = predictors.classify(classifier)

download_image(classified, "/path/to/classified.tif", roi, scale=10, dtype="uint8")
print("Classification complete.")
```
