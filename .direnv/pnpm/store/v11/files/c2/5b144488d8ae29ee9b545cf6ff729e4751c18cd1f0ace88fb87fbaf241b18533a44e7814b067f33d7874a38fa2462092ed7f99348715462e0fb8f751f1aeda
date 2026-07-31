import { nelderMead, bisect, conjugateGradient, zeros, zerosM, norm2, scale } from 'fmin';
import { intersectionArea, circleOverlap, circleCircleIntersection, distance } from './circleintersection';

/**
 * given a list of set objects, and their corresponding overlaps
 * updates the (x, y, radius) attribute on each set such that their positions
 * roughly correspond to the desired overlaps
 * @param {readonly {sets: readonly string[]; size: number; weight?: number}[]} sets
 * @returns {{[setid: string]: {x: number, y: number, radius: number}}}
 */
export function venn(sets, parameters = {}) {
  parameters.maxIterations = parameters.maxIterations || 500;

  const initialLayout = parameters.initialLayout || bestInitialLayout;
  const loss = parameters.lossFunction || lossFunction;

  // add in missing pairwise areas as having 0 size
  const areas = addMissingAreas(sets, parameters);

  // initial layout is done greedily
  const circles = initialLayout(areas, parameters);

  // transform x/y coordinates to a vector to optimize
  const setids = Object.keys(circles);
  /** @type {number[]} */
  const initial = [];
  for (const setid of setids) {
    initial.push(circles[setid].x);
    initial.push(circles[setid].y);
  }

  // optimize initial layout from our loss function
  const solution = nelderMead(
    (values) => {
      const current = {};
      for (let i = 0; i < setids.length; ++i) {
        const setid = setids[i];
        current[setid] = {
          x: values[2 * i],
          y: values[2 * i + 1],
          radius: circles[setid].radius,
          // size : circles[setid].size
        };
      }
      return loss(current, areas);
    },
    initial,
    parameters
  );

  // transform solution vector back to x/y points
  const positions = solution.x;
  for (let i = 0; i < setids.length; ++i) {
    const setid = setids[i];
    circles[setid].x = positions[2 * i];
    circles[setid].y = positions[2 * i + 1];
  }

  return circles;
}

const SMALL = 1e-10;

/**
 * Returns the distance necessary for two circles of radius r1 + r2 to
 * have the overlap area 'overlap'
 * @param {number} r1
 * @param {number} r2
 * @param {number} overlap
 * @returns {number}
 */
export function distanceFromIntersectArea(r1, r2, overlap) {
  // handle complete overlapped circles
  if (Math.min(r1, r2) * Math.min(r1, r2) * Math.PI <= overlap + SMALL) {
    return Math.abs(r1 - r2);
  }

  return bisect((distance) => circleOverlap(r1, r2, distance) - overlap, 0, r1 + r2);
}

/**
 * Missing pair-wise intersection area data can cause problems:
 * treating as an unknown means that sets will be laid out overlapping,
 * which isn't what people expect. To reflect that we want disjoint sets
 * here, set the overlap to 0 for all missing pairwise set intersections
 * @param {ReadonlyArray<{sets: ReadonlyArray<string>, size: number}>} areas
 * @returns {ReadonlyArray<{sets: ReadonlyArray<string>, size: number}>}
 */
function addMissingAreas(areas, parameters = {}) {
  const distinct = parameters.distinct;
  const r = areas.map((s) => Object.assign({}, s));

  function toKey(arr) {
    return arr.join(';');
  }

  if (distinct) {
    // recreate the full ones by adding things up but just to level two since the rest doesn't matter
    /** @types Map<string, number> */
    const count = new Map();
    for (const area of r) {
      for (let i = 0; i < area.sets.length; i++) {
        const si = String(area.sets[i]);
        count.set(si, area.size + (count.get(si) || 0));
        for (let j = i + 1; j < area.sets.length; j++) {
          const sj = String(area.sets[j]);
          const k1 = `${si};${sj}`;
          const k2 = `${sj};${si}`;
          count.set(k1, area.size + (count.get(k1) || 0));
          count.set(k2, area.size + (count.get(k2) || 0));
        }
      }
    }
    for (const area of r) {
      if (area.sets.length < 3) {
        area.size = count.get(toKey(area.sets));
      }
    }
  }

  // two circle intersections that aren't defined
  const ids = [];

  /** @type {Set<string>} */
  const pairs = new Set();
  for (const area of r) {
    if (area.sets.length === 1) {
      ids.push(area.sets[0]);
    } else if (area.sets.length === 2) {
      const a = area.sets[0];
      const b = area.sets[1];
      pairs.add(toKey(area.sets));
      pairs.add(toKey([b, a]));
    }
  }

  ids.sort((a, b) => (a === b ? 0 : a < b ? -1 : +1));

  for (let i = 0; i < ids.length; ++i) {
    const a = ids[i];
    for (let j = i + 1; j < ids.length; ++j) {
      const b = ids[j];
      if (!pairs.has(toKey([a, b]))) {
        r.push({ sets: [a, b], size: 0 });
      }
    }
  }
  return r;
}

/**
 * Returns two matrices, one of the euclidean distances between the sets
 * and the other indicating if there are subset or disjoint set relationships
 * @param {ReadonlyArray<{sets: ReadonlyArray<number>}>} areas
 * @param {ReadonlyArray<{size: number}>} sets
 * @param {ReadonlyArray<number>} setids
 */
export function getDistanceMatrices(areas, sets, setids) {
  // initialize an empty distance matrix between all the points
  /**
   * @type {number[][]}
   */
  const distances = zerosM(sets.length, sets.length);
  /**
   * @type {number[][]}
   */
  const constraints = zerosM(sets.length, sets.length);

  // compute required distances between all the sets such that
  // the areas match
  areas
    .filter((x) => x.sets.length === 2)
    .forEach((current) => {
      const left = setids[current.sets[0]];
      const right = setids[current.sets[1]];
      const r1 = Math.sqrt(sets[left].size / Math.PI);
      const r2 = Math.sqrt(sets[right].size / Math.PI);
      const distance = distanceFromIntersectArea(r1, r2, current.size);

      distances[left][right] = distances[right][left] = distance;

      // also update constraints to indicate if its a subset or disjoint
      // relationship
      let c = 0;
      if (current.size + 1e-10 >= Math.min(sets[left].size, sets[right].size)) {
        c = 1;
      } else if (current.size <= 1e-10) {
        c = -1;
      }
      constraints[left][right] = constraints[right][left] = c;
    });

  return { distances, constraints };
}

/// computes the gradient and loss simultaneously for our constrained MDS optimizer
function constrainedMDSGradient(x, fxprime, distances, constraints) {
  for (let i = 0; i < fxprime.length; ++i) {
    fxprime[i] = 0;
  }

  let loss = 0;
  for (let i = 0; i < distances.length; ++i) {
    const xi = x[2 * i];
    const yi = x[2 * i + 1];
    for (let j = i + 1; j < distances.length; ++j) {
      const xj = x[2 * j];
      const yj = x[2 * j + 1];
      const dij = distances[i][j];
      const constraint = constraints[i][j];

      const squaredDistance = (xj - xi) * (xj - xi) + (yj - yi) * (yj - yi);
      const distance = Math.sqrt(squaredDistance);
      const delta = squaredDistance - dij * dij;

      if ((constraint > 0 && distance <= dij) || (constraint < 0 && distance >= dij)) {
        continue;
      }

      loss += 2 * delta * delta;

      fxprime[2 * i] += 4 * delta * (xi - xj);
      fxprime[2 * i + 1] += 4 * delta * (yi - yj);

      fxprime[2 * j] += 4 * delta * (xj - xi);
      fxprime[2 * j + 1] += 4 * delta * (yj - yi);
    }
  }
  return loss;
}

/**
 * takes the best working variant of either constrained MDS or greedy
 * @param {ReadonlyArray<{sets: ReadonlyArray<string>, size: number}>} areas
 */
export function bestInitialLayout(areas, params = {}) {
  let initial = greedyLayout(areas, params);
  const loss = params.lossFunction || lossFunction;

  // greedylayout is sufficient for all 2/3 circle cases. try out
  // constrained MDS for higher order problems, take its output
  // if it outperforms. (greedy is aesthetically better on 2/3 circles
  // since it axis aligns)
  if (areas.length >= 8) {
    const constrained = constrainedMDSLayout(areas, params);
    const constrainedLoss = loss(constrained, areas);
    const greedyLoss = loss(initial, areas);

    if (constrainedLoss + 1e-8 < greedyLoss) {
      initial = constrained;
    }
  }
  return initial;
}

/**
 * use the constrained MDS variant to generate an initial layout
 * @param {ReadonlyArray<{sets: ReadonlyArray<string>, size: number}>} areas
 * @returns {{[key: string]: {x: number, y: number, radius: number}}}
 */
export function constrainedMDSLayout(areas, params = {}) {
  const restarts = params.restarts || 10;

  // bidirectionally map sets to a rowid  (so we can create a matrix)
  const sets = [];
  const setids = {};
  for (const area of areas) {
    if (area.sets.length === 1) {
      setids[area.sets[0]] = sets.length;
      sets.push(area);
    }
  }

  let { distances, constraints } = getDistanceMatrices(areas, sets, setids);

  // keep distances bounded, things get messed up otherwise.
  // TODO: proper preconditioner?
  const norm = norm2(distances.map(norm2)) / distances.length;
  distances = distances.map((row) => row.map((value) => value / norm));

  const obj = (x, fxprime) => constrainedMDSGradient(x, fxprime, distances, constraints);

  let best = null;
  for (let i = 0; i < restarts; ++i) {
    const initial = zeros(distances.length * 2).map(Math.random);

    const current = conjugateGradient(obj, initial, params);
    if (!best || current.fx < best.fx) {
      best = current;
    }
  }

  const positions = best.x;

  // translate rows back to (x,y,radius) coordinates
  /** @type {{[key: string]: {x: number, y: number, radius: number}}} */
  const circles = {};
  for (let i = 0; i < sets.length; ++i) {
    const set = sets[i];
    circles[set.sets[0]] = {
      x: positions[2 * i] * norm,
      y: positions[2 * i + 1] * norm,
      radius: Math.sqrt(set.size / Math.PI),
    };
  }

  if (params.history) {
    for (const h of params.history) {
      scale(h.x, norm);
    }
  }
  return circles;
}

/**
 * Lays out a Venn diagram greedily, going from most overlapped sets to
 * least overlapped, attempting to position each new set such that the
 * overlapping areas to already positioned sets are basically right
 * @param {ReadonlyArray<{size: number, sets: ReadonlyArray<string>}>} areas
 * @return {{[key: string]: {x: number, y: number, radius: number}}}
 */
export function greedyLayout(areas, params) {
  const loss = params && params.lossFunction ? params.lossFunction : lossFunction;

  // define a circle for each set
  /** @type {{[key: string]: {x: number, y: number, radius: number}}} */
  const circles = {};
  /** @type {{[key: string]: {set: string, size: number, weight: number}[]}} */
  const setOverlaps = {};
  for (const area of areas) {
    if (area.sets.length === 1) {
      const set = area.sets[0];
      circles[set] = {
        x: 1e10,
        y: 1e10,
        rowid: circles.length,
        size: area.size,
        radius: Math.sqrt(area.size / Math.PI),
      };
      setOverlaps[set] = [];
    }
  }

  areas = areas.filter((a) => a.sets.length === 2);

  // map each set to a list of all the other sets that overlap it
  for (const current of areas) {
    let weight = current.weight != null ? current.weight : 1.0;
    const left = current.sets[0];
    const right = current.sets[1];

    // completely overlapped circles shouldn't be positioned early here
    if (current.size + SMALL >= Math.min(circles[left].size, circles[right].size)) {
      weight = 0;
    }

    setOverlaps[left].push({ set: right, size: current.size, weight });
    setOverlaps[right].push({ set: left, size: current.size, weight });
  }

  // get list of most overlapped sets
  const mostOverlapped = [];
  Object.keys(setOverlaps).forEach((set) => {
    let size = 0;
    for (let i = 0; i < setOverlaps[set].length; ++i) {
      size += setOverlaps[set][i].size * setOverlaps[set][i].weight;
    }

    mostOverlapped.push({ set, size });
  });

  // sort by size desc
  function sortOrder(a, b) {
    return b.size - a.size;
  }
  mostOverlapped.sort(sortOrder);

  // keep track of what sets have been laid out
  const positioned = {};
  function isPositioned(element) {
    return element.set in positioned;
  }

  /**
   * adds a point to the output
   * @param {{x: number, y: number}} point
   * @param {number} index
   */
  function positionSet(point, index) {
    circles[index].x = point.x;
    circles[index].y = point.y;
    positioned[index] = true;
  }

  // add most overlapped set at (0,0)
  positionSet({ x: 0, y: 0 }, mostOverlapped[0].set);

  // get distances between all points. TODO, necessary?
  // answer: probably not
  // var distances = venn.getDistanceMatrices(circles, areas).distances;
  for (let i = 1; i < mostOverlapped.length; ++i) {
    const setIndex = mostOverlapped[i].set;
    const overlap = setOverlaps[setIndex].filter(isPositioned);
    const set = circles[setIndex];
    overlap.sort(sortOrder);

    if (overlap.length === 0) {
      // this shouldn't happen anymore with addMissingAreas
      throw 'ERROR: missing pairwise overlap information';
    }

    /** @type {{x: number, y: number}[]} */
    const points = [];
    for (var j = 0; j < overlap.length; ++j) {
      // get appropriate distance from most overlapped already added set
      const p1 = circles[overlap[j].set];
      const d1 = distanceFromIntersectArea(set.radius, p1.radius, overlap[j].size);

      // sample positions at 90 degrees for maximum aesthetics
      points.push({ x: p1.x + d1, y: p1.y });
      points.push({ x: p1.x - d1, y: p1.y });
      points.push({ y: p1.y + d1, x: p1.x });
      points.push({ y: p1.y - d1, x: p1.x });

      // if we have at least 2 overlaps, then figure out where the
      // set should be positioned analytically and try those too
      for (let k = j + 1; k < overlap.length; ++k) {
        const p2 = circles[overlap[k].set];
        const d2 = distanceFromIntersectArea(set.radius, p2.radius, overlap[k].size);

        const extraPoints = circleCircleIntersection(
          { x: p1.x, y: p1.y, radius: d1 },
          { x: p2.x, y: p2.y, radius: d2 }
        );
        points.push(...extraPoints);
      }
    }

    // we have some candidate positions for the set, examine loss
    // at each position to figure out where to put it at
    let bestLoss = 1e50;
    let bestPoint = points[0];
    for (const point of points) {
      circles[setIndex].x = point.x;
      circles[setIndex].y = point.y;
      const localLoss = loss(circles, areas);
      if (localLoss < bestLoss) {
        bestLoss = localLoss;
        bestPoint = point;
      }
    }

    positionSet(bestPoint, setIndex);
  }

  return circles;
}

/**
 * Given a bunch of sets, and the desired overlaps between these sets - computes
 * the distance from the actual overlaps to the desired overlaps. Note that
 * this method ignores overlaps of more than 2 circles
 * @param {{[key: string]: <{x: number, y: number, radius: number}>}} circles
 * @param {ReadonlyArray<{size: number, sets: ReadonlyArray<string>, weight?: number}>} overlaps
 * @returns {number}
 */
export function lossFunction(circles, overlaps) {
  let output = 0;

  for (const area of overlaps) {
    if (area.sets.length === 1) {
      continue;
    }
    /** @type {number} */
    let overlap;
    if (area.sets.length === 2) {
      const left = circles[area.sets[0]];
      const right = circles[area.sets[1]];
      overlap = circleOverlap(left.radius, right.radius, distance(left, right));
    } else {
      overlap = intersectionArea(area.sets.map((d) => circles[d]));
    }

    const weight = area.weight != null ? area.weight : 1.0;
    output += weight * (overlap - area.size) * (overlap - area.size);
  }

  return output;
}

export function logRatioLossFunction(circles, overlaps) {
  let output = 0;

  for (const area of overlaps) {
    if (area.sets.length === 1) {
      continue;
    }
    /** @type {number} */
    let overlap;
    if (area.sets.length === 2) {
      const left = circles[area.sets[0]];
      const right = circles[area.sets[1]];
      overlap = circleOverlap(left.radius, right.radius, distance(left, right));
    } else {
      overlap = intersectionArea(area.sets.map((d) => circles[d]));
    }

    const weight = area.weight != null ? area.weight : 1.0;
    const differenceFromIdeal = Math.log((overlap + 1) / (area.size + 1));
    output += weight * differenceFromIdeal * differenceFromIdeal;
  }

  return output;
}

/**
 * orientates a bunch of circles to point in orientation
 * @param {{x :number, y: number, radius: number}[]} circles
 * @param {number | undefined} orientation
 * @param {((a: {x :number, y: number, radius: number}, b: {x :number, y: number, radius: number}) => number) | undefined} orientationOrder
 */
function orientateCircles(circles, orientation, orientationOrder) {
  if (orientationOrder == null) {
    circles.sort((a, b) => b.radius - a.radius);
  } else {
    circles.sort(orientationOrder);
  }

  // shift circles so largest circle is at (0, 0)
  if (circles.length > 0) {
    const largestX = circles[0].x;
    const largestY = circles[0].y;

    for (const circle of circles) {
      circle.x -= largestX;
      circle.y -= largestY;
    }
  }

  if (circles.length === 2) {
    // if the second circle is a subset of the first, arrange so that
    // it is off to one side. hack for https://github.com/benfred/venn.js/issues/120
    const dist = distance(circles[0], circles[1]);
    if (dist < Math.abs(circles[1].radius - circles[0].radius)) {
      circles[1].x = circles[0].x + circles[0].radius - circles[1].radius - 1e-10;
      circles[1].y = circles[0].y;
    }
  }

  // rotate circles so that second largest is at an angle of 'orientation'
  // from largest
  if (circles.length > 1) {
    const rotation = Math.atan2(circles[1].x, circles[1].y) - orientation;
    const c = Math.cos(rotation);
    const s = Math.sin(rotation);

    for (const circle of circles) {
      const x = circle.x;
      const y = circle.y;
      circle.x = c * x - s * y;
      circle.y = s * x + c * y;
    }
  }

  // mirror solution if third solution is above plane specified by
  // first two circles
  if (circles.length > 2) {
    let angle = Math.atan2(circles[2].x, circles[2].y) - orientation;
    while (angle < 0) {
      angle += 2 * Math.PI;
    }
    while (angle > 2 * Math.PI) {
      angle -= 2 * Math.PI;
    }
    if (angle > Math.PI) {
      const slope = circles[1].y / (1e-10 + circles[1].x);
      for (const circle of circles) {
        var d = (circle.x + slope * circle.y) / (1 + slope * slope);
        circle.x = 2 * d - circle.x;
        circle.y = 2 * d * slope - circle.y;
      }
    }
  }
}

/**
 *
 * @param {ReadonlyArray<{x: number, y: number, radius: number}>} circles
 * @returns {{x: number, y: number, radius: number}[][]}
 */
export function disjointCluster(circles) {
  // union-find clustering to get disjoint sets
  circles.forEach((circle) => {
    circle.parent = circle;
  });

  // path compression step in union find
  function find(circle) {
    if (circle.parent !== circle) {
      circle.parent = find(circle.parent);
    }
    return circle.parent;
  }

  function union(x, y) {
    const xRoot = find(x);
    const yRoot = find(y);
    xRoot.parent = yRoot;
  }

  // get the union of all overlapping sets
  for (let i = 0; i < circles.length; ++i) {
    for (let j = i + 1; j < circles.length; ++j) {
      const maxDistance = circles[i].radius + circles[j].radius;
      if (distance(circles[i], circles[j]) + 1e-10 < maxDistance) {
        union(circles[j], circles[i]);
      }
    }
  }

  // find all the disjoint clusters and group them together
  /** @type {Map<string, {x: number, y: number, radius: number}[]>} */
  const disjointClusters = new Map();
  for (let i = 0; i < circles.length; ++i) {
    const setid = find(circles[i]).parent.setid;
    if (!disjointClusters.has(setid)) {
      disjointClusters.set(setid, []);
    }
    disjointClusters.get(setid).push(circles[i]);
  }

  // cleanup bookkeeping
  circles.forEach((circle) => {
    delete circle.parent;
  });

  // return in more usable form
  return Array.from(disjointClusters.values());
}

/**
 * @param {ReadonlyArray<{x :number, y: number, radius: number}>} circles
 * @returns {{xRange: [number, number], yRange: [number, number]}}
 */
function getBoundingBox(circles) {
  const minMax = (d) => {
    const hi = circles.reduce((acc, c) => Math.max(acc, c[d] + c.radius), Number.NEGATIVE_INFINITY);
    const lo = circles.reduce((acc, c) => Math.min(acc, c[d] - c.radius), Number.POSITIVE_INFINITY);
    return { max: hi, min: lo };
  };
  return { xRange: minMax('x'), yRange: minMax('y') };
}

/**
 *
 * @param {{[setid: string]: {x: number, y: number, radius: number}}} solution
 * @param {undefined | number} orientation
 * @param {((a: {x :number, y: number, radius: number}, b: {x :number, y: number, radius: number}) => number) | undefined} orientationOrder
 * @returns {{[setid: string]: {x: number, y: number, radius: number}}}
 */
export function normalizeSolution(solution, orientation, orientationOrder) {
  if (orientation == null) {
    orientation = Math.PI / 2;
  }

  // work with a list instead of a dictionary, and take a copy so we
  // don't mutate input
  let circles = fromObjectNotation(solution).map((d) => Object.assign({}, d));

  // get all the disjoint clusters
  const clusters = disjointCluster(circles);

  // orientate all disjoint sets, get sizes
  for (const cluster of clusters) {
    orientateCircles(cluster, orientation, orientationOrder);
    const bounds = getBoundingBox(cluster);
    cluster.size = (bounds.xRange.max - bounds.xRange.min) * (bounds.yRange.max - bounds.yRange.min);
    cluster.bounds = bounds;
  }
  clusters.sort((a, b) => b.size - a.size);

  // orientate the largest at 0,0, and get the bounds
  circles = clusters[0];
  let returnBounds = circles.bounds;
  const spacing = (returnBounds.xRange.max - returnBounds.xRange.min) / 50;

  /**
   * @param {ReadonlyArray<{x: number, y: number, radius: number, setid: string}>} cluster
   * @param {boolean} right
   * @param {boolean} bottom
   */
  function addCluster(cluster, right, bottom) {
    if (!cluster) {
      return;
    }

    const bounds = cluster.bounds;
    /** @type {number} */
    let xOffset;
    /** @type {number} */
    let yOffset;

    if (right) {
      xOffset = returnBounds.xRange.max - bounds.xRange.min + spacing;
    } else {
      xOffset = returnBounds.xRange.max - bounds.xRange.max;
      const centreing =
        (bounds.xRange.max - bounds.xRange.min) / 2 - (returnBounds.xRange.max - returnBounds.xRange.min) / 2;
      if (centreing < 0) {
        xOffset += centreing;
      }
    }

    if (bottom) {
      yOffset = returnBounds.yRange.max - bounds.yRange.min + spacing;
    } else {
      yOffset = returnBounds.yRange.max - bounds.yRange.max;
      const centreing =
        (bounds.yRange.max - bounds.yRange.min) / 2 - (returnBounds.yRange.max - returnBounds.yRange.min) / 2;
      if (centreing < 0) {
        yOffset += centreing;
      }
    }

    for (const c of cluster) {
      c.x += xOffset;
      c.y += yOffset;
      circles.push(c);
    }
  }

  let index = 1;
  while (index < clusters.length) {
    addCluster(clusters[index], true, false);
    addCluster(clusters[index + 1], false, true);
    addCluster(clusters[index + 2], true, true);
    index += 3;

    // have one cluster (in top left). lay out next three relative
    // to it in a grid
    returnBounds = getBoundingBox(circles);
  }

  // convert back to solution form
  return toObjectNotation(circles);
}

/**
 * Scales a solution from venn.venn or venn.greedyLayout such that it fits in
 * a rectangle of width/height - with padding around the borders. also
 * centers the diagram in the available space at the same time.
 * If the scale parameter is not null, this automatic scaling is ignored in favor of this custom one
 * @param {{[setid: string]: {x: number, y: number, radius: number}}} solution
 * @param {number} width
 * @param {number} height
 * @param {number} padding
 * @param {boolean} scaleToFit
 * @returns {{[setid: string]: {x: number, y: number, radius: number}}}
 */
export function scaleSolution(solution, width, height, padding, scaleToFit) {
  const circles = fromObjectNotation(solution);

  width -= 2 * padding;
  height -= 2 * padding;

  const { xRange, yRange } = getBoundingBox(circles);

  if (xRange.max === xRange.min || yRange.max === yRange.min) {
    console.log('not scaling solution: zero size detected');
    return solution;
  }

  /** @type {number} */
  let xScaling;
  /** @type {number} */
  let yScaling;
  if (scaleToFit) {
    const toScaleDiameter = Math.sqrt(scaleToFit / Math.PI) * 2;
    xScaling = width / toScaleDiameter;
    yScaling = height / toScaleDiameter;
  } else {
    xScaling = width / (xRange.max - xRange.min);
    yScaling = height / (yRange.max - yRange.min);
  }

  const scaling = Math.min(yScaling, xScaling);
  // while we're at it, center the diagram too
  const xOffset = (width - (xRange.max - xRange.min) * scaling) / 2;
  const yOffset = (height - (yRange.max - yRange.min) * scaling) / 2;

  return toObjectNotation(
    circles.map((circle) => ({
      radius: scaling * circle.radius,
      x: padding + xOffset + (circle.x - xRange.min) * scaling,
      y: padding + yOffset + (circle.y - yRange.min) * scaling,
      setid: circle.setid,
    }))
  );
}

/**
 * @param {readonly {x: number, y: number, radius: number, setid: string}[]} circles
 * @returns {{[setid: string]: {x: number, y: number, radius: number, setid: string}}}
 */
function toObjectNotation(circles) {
  /** @type {{[setid: string]: {x: number, y: number, radius: number, setid: string}}} */
  const r = {};
  for (const circle of circles) {
    r[circle.setid] = circle;
  }
  return r;
}
/**
 * @param {{[setid: string]: {x: number, y: number, radius: number}}} solution
 * @returns {{x: number, y: number, radius: number, setid: string}[]}}
 */
function fromObjectNotation(solution) {
  const setids = Object.keys(solution);
  return setids.map((id) => Object.assign(solution[id], { setid: id }));
}
