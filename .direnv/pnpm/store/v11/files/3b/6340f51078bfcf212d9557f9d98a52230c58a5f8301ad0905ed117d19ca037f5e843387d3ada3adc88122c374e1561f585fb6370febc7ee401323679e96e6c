const SMALL$1 = 1e-10;

/**
 * Returns the intersection area of a bunch of circles (where each circle
 * is an object having an x,y and radius property)
 * @param {ReadonlyArray<{x: number, y: number, radius: number}>} circles
 * @param {undefined | { area?: number, arcArea?: number, polygonArea?: number, arcs?: ReadonlyArray<{ circle: {x: number, y: number, radius: number}, width: number, p1: {x: number, y: number}, p2: {x: number, y: number} }>, innerPoints: ReadonlyArray<{
    x: number;
    y: number;
    parentIndex: [number, number];
}>, intersectionPoints: ReadonlyArray<{
  x: number;
  y: number;
  parentIndex: [number, number];
}> }} stats
 * @returns {number}
 */
function intersectionArea(circles, stats) {
  // get all the intersection points of the circles
  const intersectionPoints = getIntersectionPoints(circles);

  // filter out points that aren't included in all the circles
  const innerPoints = intersectionPoints.filter((p) => containedInCircles(p, circles));

  let arcArea = 0;
  let polygonArea = 0;
  /** @type {{ circle: {x: number, y: number, radius: number}, width: number, p1: {x: number, y: number}, p2: {x: number, y: number} }[]} */
  const arcs = [];

  // if we have intersection points that are within all the circles,
  // then figure out the area contained by them
  if (innerPoints.length > 1) {
    // sort the points by angle from the center of the polygon, which lets
    // us just iterate over points to get the edges
    const center = getCenter(innerPoints);
    for (let i = 0; i < innerPoints.length; ++i) {
      const p = innerPoints[i];
      p.angle = Math.atan2(p.x - center.x, p.y - center.y);
    }
    innerPoints.sort((a, b) => b.angle - a.angle);

    // iterate over all points, get arc between the points
    // and update the areas
    let p2 = innerPoints[innerPoints.length - 1];
    for (let i = 0; i < innerPoints.length; ++i) {
      const p1 = innerPoints[i];

      // polygon area updates easily ...
      polygonArea += (p2.x + p1.x) * (p1.y - p2.y);

      // updating the arc area is a little more involved
      const midPoint = { x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2 };
      /** @types null | { circle: {x: number, y: number, radius: number}, width: number, p1: {x: number, y: number}, p2: {x: number, y: number} } */
      let arc = null;

      for (let j = 0; j < p1.parentIndex.length; ++j) {
        if (p2.parentIndex.includes(p1.parentIndex[j])) {
          // figure out the angle halfway between the two points
          // on the current circle
          const circle = circles[p1.parentIndex[j]];
          const a1 = Math.atan2(p1.x - circle.x, p1.y - circle.y);
          const a2 = Math.atan2(p2.x - circle.x, p2.y - circle.y);

          let angleDiff = a2 - a1;
          if (angleDiff < 0) {
            angleDiff += 2 * Math.PI;
          }

          // and use that angle to figure out the width of the
          // arc
          const a = a2 - angleDiff / 2;
          let width = distance(midPoint, {
            x: circle.x + circle.radius * Math.sin(a),
            y: circle.y + circle.radius * Math.cos(a),
          });

          // clamp the width to the largest is can actually be
          // (sometimes slightly overflows because of FP errors)
          if (width > circle.radius * 2) {
            width = circle.radius * 2;
          }

          // pick the circle whose arc has the smallest width
          if (arc == null || arc.width > width) {
            arc = { circle, width, p1, p2, large: width > circle.radius, sweep: true };
          }
        }
      }

      if (arc != null) {
        arcs.push(arc);
        arcArea += circleArea(arc.circle.radius, arc.width);
        p2 = p1;
      }
    }
  } else {
    // no intersection points, is either disjoint - or is completely
    // overlapped. figure out which by examining the smallest circle
    let smallest = circles[0];
    for (let i = 1; i < circles.length; ++i) {
      if (circles[i].radius < smallest.radius) {
        smallest = circles[i];
      }
    }

    // make sure the smallest circle is completely contained in all
    // the other circles
    let disjoint = false;
    for (let i = 0; i < circles.length; ++i) {
      if (distance(circles[i], smallest) > Math.abs(smallest.radius - circles[i].radius)) {
        disjoint = true;
        break;
      }
    }

    if (disjoint) {
      arcArea = polygonArea = 0;
    } else {
      arcArea = smallest.radius * smallest.radius * Math.PI;
      arcs.push({
        circle: smallest,
        p1: { x: smallest.x, y: smallest.y + smallest.radius },
        p2: { x: smallest.x - SMALL$1, y: smallest.y + smallest.radius },
        width: smallest.radius * 2,
        large: true,
        sweep: true,
      });
    }
  }

  polygonArea /= 2;

  if (stats) {
    stats.area = arcArea + polygonArea;
    stats.arcArea = arcArea;
    stats.polygonArea = polygonArea;
    stats.arcs = arcs;
    stats.innerPoints = innerPoints;
    stats.intersectionPoints = intersectionPoints;
  }

  return arcArea + polygonArea;
}

/**
 * returns whether a point is contained by all of a list of circles
 * @param {{x: number, y: number}} point
 * @param {ReadonlyArray<{x: number, y: number, radius: number}>} circles
 * @returns {boolean}
 */
function containedInCircles(point, circles) {
  return circles.every((circle) => distance(point, circle) < circle.radius + SMALL$1);
}

/**
 * Gets all intersection points between a bunch of circles
 * @param {ReadonlyArray<{x: number, y: number, radius: number}>} circles
 * @returns {ReadonlyArray<{x: number, y: number, parentIndex: [number, number]}>}
 */
function getIntersectionPoints(circles) {
  /** @type {{x: number, y: number, parentIndex: [number, number]}[]} */
  const ret = [];
  for (let i = 0; i < circles.length; ++i) {
    for (let j = i + 1; j < circles.length; ++j) {
      const intersect = circleCircleIntersection(circles[i], circles[j]);
      for (const p of intersect) {
        p.parentIndex = [i, j];
        ret.push(p);
      }
    }
  }
  return ret;
}

/**
 * Circular segment area calculation. See http://mathworld.wolfram.com/CircularSegment.html
 * @param {number} r
 * @param {number} width
 * @returns {number}
 **/
function circleArea(r, width) {
  return r * r * Math.acos(1 - width / r) - (r - width) * Math.sqrt(width * (2 * r - width));
}

/**
 * euclidean distance between two points
 * @param {{x: number, y: number}} p1
 * @param {{x: number, y: number}} p2
 * @returns {number}
 **/
function distance(p1, p2) {
  return Math.sqrt((p1.x - p2.x) * (p1.x - p2.x) + (p1.y - p2.y) * (p1.y - p2.y));
}

/**
 * Returns the overlap area of two circles of radius r1 and r2 - that
 * have their centers separated by distance d. Simpler faster
 * circle intersection for only two circles
 * @param {number} r1
 * @param {number} r2
 * @param {number} d
 * @returns {number}
 */
function circleOverlap(r1, r2, d) {
  // no overlap
  if (d >= r1 + r2) {
    return 0;
  }

  // completely overlapped
  if (d <= Math.abs(r1 - r2)) {
    return Math.PI * Math.min(r1, r2) * Math.min(r1, r2);
  }

  const w1 = r1 - (d * d - r2 * r2 + r1 * r1) / (2 * d);
  const w2 = r2 - (d * d - r1 * r1 + r2 * r2) / (2 * d);
  return circleArea(r1, w1) + circleArea(r2, w2);
}

/**
 * Given two circles (containing a x/y/radius attributes),
 * returns the intersecting points if possible
 * note: doesn't handle cases where there are infinitely many
 * intersection points (circles are equivalent):, or only one intersection point
 * @param {{x: number, y: number, radius: number}} p1
 * @param {{x: number, y: number, radius: number}} p2
 * @returns {ReadonlyArray<{x: number, y: number}>}
 **/
function circleCircleIntersection(p1, p2) {
  const d = distance(p1, p2);
  const r1 = p1.radius;
  const r2 = p2.radius;

  // if to far away, or self contained - can't be done
  if (d >= r1 + r2 || d <= Math.abs(r1 - r2)) {
    return [];
  }

  const a = (r1 * r1 - r2 * r2 + d * d) / (2 * d);
  const h = Math.sqrt(r1 * r1 - a * a);
  const x0 = p1.x + (a * (p2.x - p1.x)) / d;
  const y0 = p1.y + (a * (p2.y - p1.y)) / d;
  const rx = -(p2.y - p1.y) * (h / d);
  const ry = -(p2.x - p1.x) * (h / d);

  return [
    { x: x0 + rx, y: y0 - ry },
    { x: x0 - rx, y: y0 + ry },
  ];
}

/**
 * Returns the center of a bunch of points
 * @param {ReadonlyArray<{x: number, y: number}>} points
 * @returns {{x: number, y: number}}
 */
function getCenter(points) {
  const center = { x: 0, y: 0 };
  for (const point of points) {
    center.x += point.x;
    center.y += point.y;
  }
  center.x /= points.length;
  center.y /= points.length;
  return center;
}

/** finds the zeros of a function, given two starting points (which must
 * have opposite signs */
function bisect(f, a, b, parameters) {
    parameters = parameters || {};
    const maxIterations = parameters.maxIterations || 100;
    const tolerance = parameters.tolerance || 1e-10;
    const fA = f(a);
    const fB = f(b);
    let delta = b - a;

    if (fA * fB > 0) {
        throw 'Initial bisect points must have opposite signs';
    }

    if (fA === 0) return a;
    if (fB === 0) return b;

    for (let i = 0; i < maxIterations; ++i) {
        delta /= 2;
        const mid = a + delta;
        const fMid = f(mid);

        if (fMid * fA >= 0) {
            a = mid;
        }

        if (Math.abs(delta) < tolerance || fMid === 0) {
            return mid;
        }
    }
    return a + delta;
}

// need some basic operations on vectors, rather than adding a dependency,
// just define here
function zeros(x) {
    const r = new Array(x);
    for (let i = 0; i < x; ++i) {
        r[i] = 0;
    }
    return r;
}
function zerosM(x, y) {
    return zeros(x).map(() => zeros(y));
}

function dot(a, b) {
    let ret = 0;
    for (let i = 0; i < a.length; ++i) {
        ret += a[i] * b[i];
    }
    return ret;
}

function norm2(a) {
    return Math.sqrt(dot(a, a));
}

function scale(ret, value, c) {
    for (let i = 0; i < value.length; ++i) {
        ret[i] = value[i] * c;
    }
}

function weightedSum(ret, w1, v1, w2, v2) {
    for (let j = 0; j < ret.length; ++j) {
        ret[j] = w1 * v1[j] + w2 * v2[j];
    }
}

/** minimizes a function using the downhill simplex method */
function nelderMead(f, x0, parameters) {
    parameters = parameters || {};

    const maxIterations = parameters.maxIterations || x0.length * 200;
    const nonZeroDelta = parameters.nonZeroDelta || 1.05;
    const zeroDelta = parameters.zeroDelta || 0.001;
    const minErrorDelta = parameters.minErrorDelta || 1e-6;
    const minTolerance = parameters.minErrorDelta || 1e-5;
    const rho = parameters.rho !== undefined ? parameters.rho : 1;
    const chi = parameters.chi !== undefined ? parameters.chi : 2;
    const psi = parameters.psi !== undefined ? parameters.psi : -0.5;
    const sigma = parameters.sigma !== undefined ? parameters.sigma : 0.5;
    let maxDiff;

    // initialize simplex.
    const N = x0.length;
    const simplex = new Array(N + 1);
    simplex[0] = x0;
    simplex[0].fx = f(x0);
    simplex[0].id = 0;
    for (let i = 0; i < N; ++i) {
        const point = x0.slice();
        point[i] = point[i] ? point[i] * nonZeroDelta : zeroDelta;
        simplex[i + 1] = point;
        simplex[i + 1].fx = f(point);
        simplex[i + 1].id = i + 1;
    }

    function updateSimplex(value) {
        for (let i = 0; i < value.length; i++) {
            simplex[N][i] = value[i];
        }
        simplex[N].fx = value.fx;
    }

    const sortOrder = (a, b) => a.fx - b.fx;

    const centroid = x0.slice();
    const reflected = x0.slice();
    const contracted = x0.slice();
    const expanded = x0.slice();

    for (let iteration = 0; iteration < maxIterations; ++iteration) {
        simplex.sort(sortOrder);

        if (parameters.history) {
            // copy the simplex (since later iterations will mutate) and
            // sort it to have a consistent order between iterations
            const sortedSimplex = simplex.map((x) => {
                const state = x.slice();
                state.fx = x.fx;
                state.id = x.id;
                return state;
            });
            sortedSimplex.sort((a, b) => a.id - b.id);

            parameters.history.push({
                x: simplex[0].slice(),
                fx: simplex[0].fx,
                simplex: sortedSimplex,
            });
        }

        maxDiff = 0;
        for (let i = 0; i < N; ++i) {
            maxDiff = Math.max(maxDiff, Math.abs(simplex[0][i] - simplex[1][i]));
        }

        if (Math.abs(simplex[0].fx - simplex[N].fx) < minErrorDelta && maxDiff < minTolerance) {
            break;
        }

        // compute the centroid of all but the worst point in the simplex
        for (let i = 0; i < N; ++i) {
            centroid[i] = 0;
            for (let j = 0; j < N; ++j) {
                centroid[i] += simplex[j][i];
            }
            centroid[i] /= N;
        }

        // reflect the worst point past the centroid  and compute loss at reflected
        // point
        const worst = simplex[N];
        weightedSum(reflected, 1 + rho, centroid, -rho, worst);
        reflected.fx = f(reflected);

        // if the reflected point is the best seen, then possibly expand
        if (reflected.fx < simplex[0].fx) {
            weightedSum(expanded, 1 + chi, centroid, -chi, worst);
            expanded.fx = f(expanded);
            if (expanded.fx < reflected.fx) {
                updateSimplex(expanded);
            } else {
                updateSimplex(reflected);
            }
        }

        // if the reflected point is worse than the second worst, we need to
        // contract
        else if (reflected.fx >= simplex[N - 1].fx) {
            let shouldReduce = false;

            if (reflected.fx > worst.fx) {
                // do an inside contraction
                weightedSum(contracted, 1 + psi, centroid, -psi, worst);
                contracted.fx = f(contracted);
                if (contracted.fx < worst.fx) {
                    updateSimplex(contracted);
                } else {
                    shouldReduce = true;
                }
            } else {
                // do an outside contraction
                weightedSum(contracted, 1 - psi * rho, centroid, psi * rho, worst);
                contracted.fx = f(contracted);
                if (contracted.fx < reflected.fx) {
                    updateSimplex(contracted);
                } else {
                    shouldReduce = true;
                }
            }

            if (shouldReduce) {
                // if we don't contract here, we're done
                if (sigma >= 1) break;

                // do a reduction
                for (let i = 1; i < simplex.length; ++i) {
                    weightedSum(simplex[i], 1 - sigma, simplex[0], sigma, simplex[i]);
                    simplex[i].fx = f(simplex[i]);
                }
            }
        } else {
            updateSimplex(reflected);
        }
    }

    simplex.sort(sortOrder);
    return { fx: simplex[0].fx, x: simplex[0] };
}

/// searches along line 'pk' for a point that satifies the wolfe conditions
/// See 'Numerical Optimization' by Nocedal and Wright p59-60
/// f : objective function
/// pk : search direction
/// current: object containing current gradient/loss
/// next: output: contains next gradient/loss
/// returns a: step size taken
function wolfeLineSearch(f, pk, current, next, a, c1, c2) {
    const phi0 = current.fx;
    const phiPrime0 = dot(current.fxprime, pk);
    let phi = phi0;
    let phi_old = phi0;
    let phiPrime = phiPrime0;
    let a0 = 0;

    a = a || 1;
    c1 = c1 || 1e-6;
    c2 = c2 || 0.1;

    function zoom(a_lo, a_high, phi_lo) {
        for (let iteration = 0; iteration < 16; ++iteration) {
            a = (a_lo + a_high) / 2;
            weightedSum(next.x, 1.0, current.x, a, pk);
            phi = next.fx = f(next.x, next.fxprime);
            phiPrime = dot(next.fxprime, pk);

            if (phi > phi0 + c1 * a * phiPrime0 || phi >= phi_lo) {
                a_high = a;
            } else {
                if (Math.abs(phiPrime) <= -c2 * phiPrime0) {
                    return a;
                }

                if (phiPrime * (a_high - a_lo) >= 0) {
                    a_high = a_lo;
                }

                a_lo = a;
                phi_lo = phi;
            }
        }

        return 0;
    }

    for (let iteration = 0; iteration < 10; ++iteration) {
        weightedSum(next.x, 1.0, current.x, a, pk);
        phi = next.fx = f(next.x, next.fxprime);
        phiPrime = dot(next.fxprime, pk);
        if (phi > phi0 + c1 * a * phiPrime0 || (iteration && phi >= phi_old)) {
            return zoom(a0, a, phi_old);
        }

        if (Math.abs(phiPrime) <= -c2 * phiPrime0) {
            return a;
        }

        if (phiPrime >= 0) {
            return zoom(a, a0, phi);
        }

        phi_old = phi;
        a0 = a;
        a *= 2;
    }

    return a;
}

function conjugateGradient(f, initial, params) {
    // allocate all memory up front here, keep out of the loop for perfomance
    // reasons
    let current = { x: initial.slice(), fx: 0, fxprime: initial.slice() };
    let next = { x: initial.slice(), fx: 0, fxprime: initial.slice() };
    const yk = initial.slice();
    let pk;
    let temp;
    let a = 1;
    let maxIterations;

    params = params || {};
    maxIterations = params.maxIterations || initial.length * 20;

    current.fx = f(current.x, current.fxprime);
    pk = current.fxprime.slice();
    scale(pk, current.fxprime, -1);

    for (let i = 0; i < maxIterations; ++i) {
        a = wolfeLineSearch(f, pk, current, next, a);

        // todo: history in wrong spot?
        if (params.history) {
            params.history.push({
                x: current.x.slice(),
                fx: current.fx,
                fxprime: current.fxprime.slice(),
                alpha: a,
            });
        }

        if (!a) {
            // faiiled to find point that satifies wolfe conditions.
            // reset direction for next iteration
            scale(pk, current.fxprime, -1);
        } else {
            // update direction using Polak–Ribiere CG method
            weightedSum(yk, 1, next.fxprime, -1, current.fxprime);

            const delta_k = dot(current.fxprime, current.fxprime);
            const beta_k = Math.max(0, dot(yk, next.fxprime) / delta_k);

            weightedSum(pk, beta_k, pk, -1, next.fxprime);

            temp = current;
            current = next;
            next = temp;
        }

        if (norm2(current.fxprime) <= 1e-5) {
            break;
        }
    }

    if (params.history) {
        params.history.push({
            x: current.x.slice(),
            fx: current.fx,
            fxprime: current.fxprime.slice(),
            alpha: a,
        });
    }

    return current;
}

/**
 * given a list of set objects, and their corresponding overlaps
 * updates the (x, y, radius) attribute on each set such that their positions
 * roughly correspond to the desired overlaps
 * @param {readonly {sets: readonly string[]; size: number; weight?: number}[]} sets
 * @returns {{[setid: string]: {x: number, y: number, radius: number}}}
 */
function venn(sets, parameters = {}) {
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
function distanceFromIntersectArea(r1, r2, overlap) {
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
function getDistanceMatrices(areas, sets, setids) {
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
function bestInitialLayout(areas, params = {}) {
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
function constrainedMDSLayout(areas, params = {}) {
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
function greedyLayout(areas, params) {
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
function lossFunction(circles, overlaps) {
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

function logRatioLossFunction(circles, overlaps) {
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
function disjointCluster(circles) {
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
function normalizeSolution(solution, orientation, orientationOrder) {
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
function scaleSolution(solution, width, height, padding, scaleToFit) {
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

/**
 * VennDiagram includes an optional `options` parameter containing the following option(s):
 *
 * `colourScheme: Array<String>`
 * A list of color values to be applied when coloring diagram circles.
 *
 * `symmetricalTextCentre: Boolean`
 * Whether to symmetrically center each circle's text horizontally and vertically.
 * Defaults to `false`.
 *
 * `textFill: String`
 * The color to be applied to the text within each circle.
 *
 * @param {object} options
 */
function VennDiagram(options = {}) {
  let useViewBox = false,
    width = 600,
    height = 350,
    padding = 15,
    duration = 1000,
    orientation = Math.PI / 2,
    normalize = true,
    scaleToFit = null,
    wrap = true,
    styled = true,
    fontSize = null,
    orientationOrder = null,
    distinct = false,
    round = null,
    symmetricalTextCentre = options && options.symmetricalTextCentre ? options.symmetricalTextCentre : false,
    // mimic the behaviour of d3.scale.category10 from the previous
    // version of d3
    colourMap = {},
    // so this is the same as d3.schemeCategory10, which is only defined in d3 4.0
    // since we can support older versions of d3 as long as we don't force this,
    // I'm hackily redefining below. TODO: remove this and change to d3.schemeCategory10
    colourScheme =
      options && options.colourScheme
        ? options.colourScheme
        : options && options.colorScheme
          ? options.colorScheme
          : [
              '#1f77b4',
              '#ff7f0e',
              '#2ca02c',
              '#d62728',
              '#9467bd',
              '#8c564b',
              '#e377c2',
              '#7f7f7f',
              '#bcbd22',
              '#17becf',
            ],
    colourIndex = 0,
    colours = function (key) {
      if (key in colourMap) {
        return colourMap[key];
      }
      var ret = (colourMap[key] = colourScheme[colourIndex]);
      colourIndex += 1;
      if (colourIndex >= colourScheme.length) {
        colourIndex = 0;
      }
      return ret;
    },
    layoutFunction = venn,
    loss = lossFunction;

  function chart(selection) {
    let data = selection.datum();

    // handle 0-sized sets by removing from input
    const toRemove = new Set();
    data.forEach((datum) => {
      if (datum.size == 0 && datum.sets.length == 1) {
        toRemove.add(datum.sets[0]);
      }
    });
    data = data.filter((datum) => !datum.sets.some((set) => toRemove.has(set)));

    let circles = {};
    let textCentres = {};

    if (data.length > 0) {
      let solution = layoutFunction(data, { lossFunction: loss, distinct });

      if (normalize) {
        solution = normalizeSolution(solution, orientation, orientationOrder);
      }

      circles = scaleSolution(solution, width, height, padding, scaleToFit);
      textCentres = computeTextCentres(circles, data, symmetricalTextCentre);
    }

    // Figure out the current label for each set. These can change
    // and D3 won't necessarily update (fixes https://github.com/benfred/venn.js/issues/103)
    const labels = {};
    data.forEach((datum) => {
      if (datum.label) {
        labels[datum.sets] = datum.label;
      }
    });

    function label(d) {
      if (d.sets in labels) {
        return labels[d.sets];
      }
      if (d.sets.length == 1) {
        return '' + d.sets[0];
      }
    }

    // create svg if not already existing
    selection.selectAll('svg').data([circles]).enter().append('svg');

    const svg = selection.select('svg');

    if (useViewBox) {
      svg.attr('viewBox', `0 0 ${width} ${height}`);
    } else {
      svg.attr('width', width).attr('height', height);
    }

    // to properly transition intersection areas, we need the
    // previous circles locations. load from elements
    const previous = {};
    let hasPrevious = false;
    svg.selectAll('.venn-area path').each(function (d) {
      const path = this.getAttribute('d');
      if (d.sets.length == 1 && path && !distinct) {
        hasPrevious = true;
        previous[d.sets[0]] = circleFromPath(path);
      }
    });
    // interpolate intersection area paths between previous and
    // current paths
    function pathTween(d) {
      return (t) => {
        const c = d.sets.map((set) => {
          let start = previous[set];
          let end = circles[set];
          if (!start) {
            start = { x: width / 2, y: height / 2, radius: 1 };
          }
          if (!end) {
            end = { x: width / 2, y: height / 2, radius: 1 };
          }
          return {
            x: start.x * (1 - t) + end.x * t,
            y: start.y * (1 - t) + end.y * t,
            radius: start.radius * (1 - t) + end.radius * t,
          };
        });
        return intersectionAreaPath(c, round);
      };
    }

    // update data, joining on the set ids
    const nodes = svg.selectAll('.venn-area').data(data, (d) => d.sets);

    // create new nodes
    const enter = nodes
      .enter()
      .append('g')
      .attr(
        'class',
        (d) =>
          `venn-area venn-${d.sets.length == 1 ? 'circle' : 'intersection'}${
            d.colour || d.color ? ' venn-coloured' : ''
          }`
      )
      .attr('data-venn-sets', (d) => d.sets.join('_'));

    const enterPath = enter.append('path');
    const enterText = enter
      .append('text')
      .attr('class', 'label')
      .text((d) => label(d))
      .attr('text-anchor', 'middle')
      .attr('dy', '.35em')
      .attr('x', width / 2)
      .attr('y', height / 2);

    // apply minimal style if wanted
    if (styled) {
      enterPath
        .style('fill-opacity', '0')
        .filter((d) => d.sets.length == 1)
        .style('fill', (d) => (d.colour ? d.colour : d.color ? d.color : colours(d.sets)))
        .style('fill-opacity', '.25');

      enterText.style('fill', (d) => {
        if (d.colour || d.color) {
          return '#FFF';
        }
        if (options.textFill) {
          return options.textFill;
        }
        return d.sets.length == 1 ? colours(d.sets) : '#444';
      });
    }

    function asTransition(s) {
      if (typeof s.transition === 'function') {
        return s.transition('venn').duration(duration);
      }
      return s;
    }

    // update existing, using pathTween if necessary
    let update = selection;
    if (hasPrevious && typeof update.transition === 'function') {
      update = asTransition(selection);
      update.selectAll('path').attrTween('d', pathTween);
    } else {
      update.selectAll('path').attr('d', (d) => intersectionAreaPath(d.sets.map((set) => circles[set])), round);
    }

    const updateText = update
      .selectAll('text')
      .filter((d) => d.sets in textCentres)
      .text((d) => label(d))
      .attr('x', (d) => Math.floor(textCentres[d.sets].x))
      .attr('y', (d) => Math.floor(textCentres[d.sets].y));

    if (wrap) {
      if (hasPrevious) {
        // d3 4.0 uses 'on' for events on transitions,
        // but d3 3.0 used 'each' instead. switch appropriately
        if ('on' in updateText) {
          updateText.on('end', wrapText(circles, label));
        } else {
          updateText.each('end', wrapText(circles, label));
        }
      } else {
        updateText.each(wrapText(circles, label));
      }
    }

    // remove old
    const exit = asTransition(nodes.exit()).remove();
    if (typeof nodes.transition === 'function') {
      exit.selectAll('path').attrTween('d', pathTween);
    }

    const exitText = exit
      .selectAll('text')
      .attr('x', width / 2)
      .attr('y', height / 2);

    // if we've been passed a fontSize explicitly, use it to
    // transition
    if (fontSize !== null) {
      enterText.style('font-size', '0px');
      updateText.style('font-size', fontSize);
      exitText.style('font-size', '0px');
    }

    return { circles, textCentres, nodes, enter, update, exit };
  }

  chart.wrap = function (_) {
    if (!arguments.length) return wrap;
    wrap = _;
    return chart;
  };

  chart.useViewBox = function () {
    useViewBox = true;
    return chart;
  };

  chart.width = function (_) {
    if (!arguments.length) return width;
    width = _;
    return chart;
  };

  chart.height = function (_) {
    if (!arguments.length) return height;
    height = _;
    return chart;
  };

  chart.padding = function (_) {
    if (!arguments.length) return padding;
    padding = _;
    return chart;
  };

  chart.distinct = function (_) {
    if (!arguments.length) return distinct;
    distinct = _;
    return chart;
  };

  chart.colours = function (_) {
    if (!arguments.length) return colours;
    colours = _;
    return chart;
  };

  chart.colors = function (_) {
    if (!arguments.length) return colours;
    colours = _;
    return chart;
  };

  chart.fontSize = function (_) {
    if (!arguments.length) return fontSize;
    fontSize = _;
    return chart;
  };

  chart.round = function (_) {
    if (!arguments.length) return round;
    round = _;
    return chart;
  };

  chart.duration = function (_) {
    if (!arguments.length) return duration;
    duration = _;
    return chart;
  };

  chart.layoutFunction = function (_) {
    if (!arguments.length) return layoutFunction;
    layoutFunction = _;
    return chart;
  };

  chart.normalize = function (_) {
    if (!arguments.length) return normalize;
    normalize = _;
    return chart;
  };

  chart.scaleToFit = function (_) {
    if (!arguments.length) return scaleToFit;
    scaleToFit = _;
    return chart;
  };

  chart.styled = function (_) {
    if (!arguments.length) return styled;
    styled = _;
    return chart;
  };

  chart.orientation = function (_) {
    if (!arguments.length) return orientation;
    orientation = _;
    return chart;
  };

  chart.orientationOrder = function (_) {
    if (!arguments.length) return orientationOrder;
    orientationOrder = _;
    return chart;
  };

  chart.lossFunction = function (_) {
    if (!arguments.length) return loss;
    loss = _ === 'default' ? lossFunction : _ === 'logRatio' ? logRatioLossFunction : _;
    return chart;
  };

  return chart;
}

// sometimes text doesn't fit inside the circle, if thats the case lets wrap
// the text here such that it fits
// todo: looks like this might be merged into d3 (
// https://github.com/mbostock/d3/issues/1642),
// also worth checking out is
// http://engineering.findthebest.com/wrapping-axis-labels-in-d3-js/
// this seems to be one of those things that should be easy but isn't
function wrapText(circles, labeller) {
  return function (data) {
    const text = this;
    const width = circles[data.sets[0]].radius || 50;
    const label = labeller(data) || '';

    const words = label.split(/\s+/).reverse();
    const maxLines = 3;
    const minChars = (label.length + words.length) / maxLines;

    let word = words.pop();
    let line = [word];
    let lineNumber = 0;
    const lineHeight = 1.1; // ems
    text.textContent = null; // clear
    const tspans = [];

    function append(word) {
      const tspan = text.ownerDocument.createElementNS(text.namespaceURI, 'tspan');
      tspan.textContent = word;
      tspans.push(tspan);
      text.append(tspan);
      return tspan;
    }
    let tspan = append(word);

    while (true) {
      word = words.pop();
      if (!word) {
        break;
      }
      line.push(word);
      const joined = line.join(' ');
      tspan.textContent = joined;
      if (joined.length > minChars && tspan.getComputedTextLength() > width) {
        line.pop();
        tspan.textContent = line.join(' ');
        line = [word];
        tspan = append(word);
        lineNumber++;
      }
    }

    const initial = 0.35 - (lineNumber * lineHeight) / 2;
    const x = text.getAttribute('x');
    const y = text.getAttribute('y');
    tspans.forEach((t, i) => {
      t.setAttribute('x', x);
      t.setAttribute('y', y);
      t.setAttribute('dy', `${initial + i * lineHeight}em`);
    });
  };
}

/**
 *
 * @param {{x: number, y: number}} current
 * @param {ReadonlyArray<{x: number, y: number}>} interior
 * @param {ReadonlyArray<{x: number, y: number}>} exterior
 * @returns {number}
 */
function circleMargin(current, interior, exterior) {
  let margin = interior[0].radius - distance(interior[0], current);

  for (let i = 1; i < interior.length; ++i) {
    const m = interior[i].radius - distance(interior[i], current);
    if (m <= margin) {
      margin = m;
    }
  }

  for (let i = 0; i < exterior.length; ++i) {
    const m = distance(exterior[i], current) - exterior[i].radius;
    if (m <= margin) {
      margin = m;
    }
  }
  return margin;
}

/**
 * compute the center of some circles by maximizing the margin of
 * the center point relative to the circles (interior) after subtracting
 * nearby circles (exterior)
 * @param {readonly {x: number, y: number, radius: number}[]} interior
 * @param {readonly {x: number, y: number, radius: number}[]} exterior
 * @param {boolean} symmetricalTextCentre
 * @returns {{x:number, y: number}}
 */
function computeTextCentre(interior, exterior, symmetricalTextCentre) {
  // get an initial estimate by sampling around the interior circles
  // and taking the point with the biggest margin
  /** @type {{x: number, y: number}[]} */
  const points = [];
  for (const c of interior) {
    points.push({ x: c.x, y: c.y });
    points.push({ x: c.x + c.radius / 2, y: c.y });
    points.push({ x: c.x - c.radius / 2, y: c.y });
    points.push({ x: c.x, y: c.y + c.radius / 2 });
    points.push({ x: c.x, y: c.y - c.radius / 2 });
  }

  let initial = points[0];
  let margin = circleMargin(points[0], interior, exterior);

  for (let i = 1; i < points.length; ++i) {
    const m = circleMargin(points[i], interior, exterior);
    if (m >= margin) {
      initial = points[i];
      margin = m;
    }
  }

  // maximize the margin numerically
  const solution = nelderMead(
    (p) => -1 * circleMargin({ x: p[0], y: p[1] }, interior, exterior),
    [initial.x, initial.y],
    { maxIterations: 500, minErrorDelta: 1e-10 }
  ).x;

  const ret = { x: symmetricalTextCentre ? 0 : solution[0], y: solution[1] };

  // check solution, fallback as needed (happens if fully overlapped
  // etc)
  let valid = true;
  for (const i of interior) {
    if (distance(ret, i) > i.radius) {
      valid = false;
      break;
    }
  }

  for (const e of exterior) {
    if (distance(ret, e) < e.radius) {
      valid = false;
      break;
    }
  }
  if (valid) {
    return ret;
  }

  if (interior.length == 1) {
    return { x: interior[0].x, y: interior[0].y };
  }
  const areaStats = {};
  intersectionArea(interior, areaStats);

  if (areaStats.arcs.length === 0) {
    return { x: 0, y: -1000, disjoint: true };
  }
  if (areaStats.arcs.length == 1) {
    return { x: areaStats.arcs[0].circle.x, y: areaStats.arcs[0].circle.y };
  }
  if (exterior.length) {
    // try again without other circles
    return computeTextCentre(interior, []);
  }
  // take average of all the points in the intersection
  // polygon. this should basically never happen
  // and has some issues:
  // https://github.com/benfred/venn.js/issues/48#issuecomment-146069777
  return getCenter(areaStats.arcs.map((a) => a.p1));
}

// given a dictionary of {setid : circle}, returns
// a dictionary of setid to list of circles that completely overlap it
function getOverlappingCircles(circles) {
  const ret = {};
  const circleids = Object.keys(circles);
  for (const circleid of circleids) {
    ret[circleid] = [];
  }
  for (let i = 0; i < circleids.length; i++) {
    const ci = circleids[i];
    const a = circles[ci];
    for (let j = i + 1; j < circleids.length; ++j) {
      const cj = circleids[j];
      const b = circles[cj];
      const d = distance(a, b);

      if (d + b.radius <= a.radius + 1e-10) {
        ret[cj].push(ci);
      } else if (d + a.radius <= b.radius + 1e-10) {
        ret[ci].push(cj);
      }
    }
  }
  return ret;
}

function computeTextCentres(circles, areas, symmetricalTextCentre) {
  const ret = {};
  const overlapped = getOverlappingCircles(circles);
  for (let i = 0; i < areas.length; ++i) {
    const area = areas[i].sets;
    const areaids = {};
    const exclude = {};

    for (let j = 0; j < area.length; ++j) {
      areaids[area[j]] = true;
      const overlaps = overlapped[area[j]];
      // keep track of any circles that overlap this area,
      // and don't consider for purposes of computing the text
      // centre
      for (let k = 0; k < overlaps.length; ++k) {
        exclude[overlaps[k]] = true;
      }
    }

    const interior = [];
    const exterior = [];
    for (let setid in circles) {
      if (setid in areaids) {
        interior.push(circles[setid]);
      } else if (!(setid in exclude)) {
        exterior.push(circles[setid]);
      }
    }
    const centre = computeTextCentre(interior, exterior, symmetricalTextCentre);
    ret[area] = centre;
    if (centre.disjoint && areas[i].size > 0) {
      console.log('WARNING: area ' + area + ' not represented on screen');
    }
  }
  return ret;
}

// sorts all areas in the venn diagram, so that
// a particular area is on top (relativeTo) - and
// all other areas are so that the smallest areas are on top
function sortAreas(div, relativeTo) {
  // figure out sets that are completely overlapped by relativeTo
  const overlaps = getOverlappingCircles(div.selectAll('svg').datum());
  const exclude = new Set();
  for (const check of relativeTo.sets) {
    for (let setid in overlaps) {
      const overlap = overlaps[setid];
      for (let j = 0; j < overlap.length; ++j) {
        if (overlap[j] == check) {
          exclude.add(setid);
          break;
        }
      }
    }
  }

  // checks that all sets are in exclude;
  function shouldExclude(sets) {
    return sets.every((set) => !exclude.has(set));
  }

  // need to sort div's so that Z order is correct
  div.selectAll('g').sort((a, b) => {
    // highest order set intersections first
    if (a.sets.length != b.sets.length) {
      return a.sets.length - b.sets.length;
    }

    if (a == relativeTo) {
      return shouldExclude(b.sets) ? -1 : 1;
    }
    if (b == relativeTo) {
      return shouldExclude(a.sets) ? 1 : -1;
    }

    // finally by size
    return b.size - a.size;
  });
}

/**
 * @param {number} x
 * @param {number} y
 * @param {number} r
 * @returns {string}
 */
function circlePath(x, y, r) {
  const ret = [];
  ret.push('\nM', x, y);
  ret.push('\nm', -r, 0);
  ret.push('\na', r, r, 0, 1, 0, r * 2, 0);
  ret.push('\na', r, r, 0, 1, 0, -r * 2, 0);
  return ret.join(' ');
}

/**
 * inverse of the circlePath function, returns a circle object from an svg path
 * @param {string} path
 * @returns {{x: number, y: number, radius: number}}
 */
function circleFromPath(path) {
  const tokens = path.split(' ');
  return { x: Number.parseFloat(tokens[1]), y: Number.parseFloat(tokens[2]), radius: -Number.parseFloat(tokens[4]) };
}

function intersectionAreaArcs(circles) {
  if (circles.length === 0) {
    return [];
  }
  const stats = {};
  intersectionArea(circles, stats);
  return stats.arcs;
}

function arcsToPath(arcs, round) {
  if (arcs.length === 0) {
    return 'M 0 0';
  }
  const rFactor = Math.pow(10, round || 0);
  const r = round != null ? (v) => Math.round(v * rFactor) / rFactor : (v) => v;
  if (arcs.length == 1) {
    const circle = arcs[0].circle;
    return circlePath(r(circle.x), r(circle.y), r(circle.radius));
  }
  // draw path around arcs
  const ret = ['\nM', r(arcs[0].p2.x), r(arcs[0].p2.y)];
  for (const arc of arcs) {
    const radius = r(arc.circle.radius);
    ret.push('\nA', radius, radius, 0, arc.large ? 1 : 0, arc.sweep ? 1 : 0, r(arc.p1.x), r(arc.p1.y));
  }
  return ret.join(' ');
}

/**
 * returns a svg path of the intersection area of a bunch of circles
 * @param {ReadonlyArray<{x: number, y: number, radius: number}>} circles
 * @returns {string}
 */
function intersectionAreaPath(circles, round) {
  return arcsToPath(intersectionAreaArcs(circles), round);
}

function layout(data, options = {}) {
  const {
    lossFunction: loss,
    layoutFunction: layout = venn,
    normalize = true,
    orientation = Math.PI / 2,
    orientationOrder,
    width = 600,
    height = 350,
    padding = 15,
    scaleToFit = false,
    symmetricalTextCentre = false,
    distinct,
    round = 2,
  } = options;

  let solution = layout(data, {
    lossFunction: loss === 'default' || !loss ? lossFunction : loss === 'logRatio' ? logRatioLossFunction : loss,
    distinct,
  });

  if (normalize) {
    solution = normalizeSolution(solution, orientation, orientationOrder);
  }

  const circles = scaleSolution(solution, width, height, padding, scaleToFit);
  const textCentres = computeTextCentres(circles, data, symmetricalTextCentre);

  const circleLookup = new Map(
    Object.keys(circles).map((set) => [
      set,
      {
        set,
        x: circles[set].x,
        y: circles[set].y,
        radius: circles[set].radius,
      },
    ])
  );
  const helpers = data.map((area) => {
    const circles = area.sets.map((s) => circleLookup.get(s));
    const arcs = intersectionAreaArcs(circles);
    const path = arcsToPath(arcs, round);
    return { circles, arcs, path, area, has: new Set(area.sets) };
  });

  function genDistinctPath(sets) {
    let r = '';
    for (const e of helpers) {
      if (e.has.size > sets.length && sets.every((s) => e.has.has(s))) {
        r += ' ' + e.path;
      }
    }
    return r;
  }

  return helpers.map(({ circles, arcs, path, area }) => {
    return {
      data: area,
      text: textCentres[area.sets],
      circles,
      arcs,
      path,
      distinctPath: path + genDistinctPath(area.sets),
    };
  });
}

export { VennDiagram, bestInitialLayout, circleArea, circleCircleIntersection, circleFromPath, circleOverlap, circlePath, computeTextCentre, computeTextCentres, disjointCluster, distance, distanceFromIntersectArea, greedyLayout, intersectionArea, intersectionAreaPath, layout, logRatioLossFunction, lossFunction, normalizeSolution, scaleSolution, sortAreas, venn, wrapText };
