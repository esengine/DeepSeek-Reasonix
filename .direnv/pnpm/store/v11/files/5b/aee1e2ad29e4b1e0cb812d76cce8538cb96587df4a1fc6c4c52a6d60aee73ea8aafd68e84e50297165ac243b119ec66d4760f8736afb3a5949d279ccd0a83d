const SMALL = 1e-10;

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
export function intersectionArea(circles, stats) {
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
        p2: { x: smallest.x - SMALL, y: smallest.y + smallest.radius },
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
export function containedInCircles(point, circles) {
  return circles.every((circle) => distance(point, circle) < circle.radius + SMALL);
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
export function circleArea(r, width) {
  return r * r * Math.acos(1 - width / r) - (r - width) * Math.sqrt(width * (2 * r - width));
}

/**
 * euclidean distance between two points
 * @param {{x: number, y: number}} p1
 * @param {{x: number, y: number}} p2
 * @returns {number}
 **/
export function distance(p1, p2) {
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
export function circleOverlap(r1, r2, d) {
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
export function circleCircleIntersection(p1, p2) {
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
export function getCenter(points) {
  const center = { x: 0, y: 0 };
  for (const point of points) {
    center.x += point.x;
    center.y += point.y;
  }
  center.x /= points.length;
  center.y /= points.length;
  return center;
}
