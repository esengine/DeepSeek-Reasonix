import { distance, intersectionArea, circleCircleIntersection, circleOverlap, circleArea } from './circleintersection';
import { describe, test, expect } from 'vitest';

describe('circleArea', () => {
  test('empty circle test', () => {
    expect(circleArea(10, 0)).toBeCloseTo(0);
  });
  test('half circle test', () => {
    expect(circleArea(10, 10)).toBeCloseTo((Math.PI * 10 * 10) / 2);
  });
  test('full circle test', () => {
    expect(circleArea(10, 20)).toBeCloseTo(Math.PI * 10 * 10);
  });
});

describe('circleOverlap', () => {
  test('non overlapping circles test', () => {
    expect(circleOverlap(10, 10, 200)).toBeCloseTo(0);
  });

  test('full overlapping circles test', () => {
    expect(circleOverlap(10, 10, 0)).toBeCloseTo(Math.PI * 10 * 10);
    expect(circleOverlap(10, 5, 5)).toBeCloseTo(Math.PI * 5 * 5);
  });
});

describe('circleCircleIntersection', () => {
  function testIntersection(p1, p2) {
    const points = circleCircleIntersection(p1, p2);
    // make sure that points are appropriately spaced
    for (let i = 0; i < points.length; i++) {
      const point = points[i];
      expect(distance(point, p1)).toBeCloseTo(p1.radius);
      expect(distance(point, p2)).toBeCloseTo(p2.radius);
    }

    return points;
  }

  test('fully contained', () => {
    expect(circleCircleIntersection({ x: 0, y: 3, radius: 10 }, { x: 3, y: 0, radius: 20 })).toHaveLength(0);
  });

  test('fully disjoint', () => {
    expect(circleCircleIntersection({ x: 0, y: 0, radius: 10 }, { x: 21, y: 0, radius: 10 })).toHaveLength(0);
  });

  test('midway between 2 points on y axis', () => {
    const points = testIntersection(
      { x: 0, y: 0, radius: 10 },
      { x: 10, y: 0, radius: 10 },
      'test midway intersection'
    );
    expect(points).toHaveLength(2);
    expect(points[0].x).toBeCloseTo(5);
    expect(points[1].x).toBeCloseTo(5);
    expect(points[0].y).toBeCloseTo(-1 * points[1].y);
  });
  test('failing case from input', () => {
    const points = testIntersection({ radius: 10, x: 15, y: 5 }, { radius: 10, x: 20, y: 0 }, 'test intersection2');
    expect(points).toHaveLength(2);
  });
});

describe('intersectionArea', () => {
  test('0', () => {
    // each one of these circles overlaps all the others, but the total overlap is still 0
    const circles = [
      { x: 0.909, y: 0.905, radius: 0.548 },
      { x: 0.765, y: 0.382, radius: 0.703 },
      { x: 0.63, y: 0.019, radius: 0.449 },
      { x: 0.21, y: 0.755, radius: 0.656 },
      { x: 0.276, y: 0.723, radius: 1.145 },
      { x: 0.141, y: 0.585, radius: 0.419 },
    ];

    const area = intersectionArea(circles);
    expect(area).toBe(0);
  });
  test('1', () => {
    // no intersection points, but the smallest circle is completely overlapped by each of the others
    const circles = [
      { x: 0.426, y: 0.882, radius: 0.944 },
      { x: 0.24, y: 0.685, radius: 0.992 },
      { x: 0.01, y: 0.909, radius: 1.161 },
      { x: 0.54, y: 0.475, radius: 0.41 },
    ];

    expect(circles[3].radius * circles[3].radius * Math.PI).toBeCloseTo(intersectionArea(circles));
  });
});

describe('randomFailures', () => {
  test('0', () => {
    const circles = [
      { x: 0.501, y: 0.32, radius: 0.629 },
      { x: 0.945, y: 0.022, radius: 1.015 },
      { x: 0.021, y: 0.863, radius: 0.261 },
      { x: 0.528, y: 0.09, radius: 0.676 },
    ];
    const area = intersectionArea(circles);

    expect(Math.abs(area - 0.0008914)).toBeLessThan(0.0001);
  });
  test('1', () => {
    const circles = [
      { x: 9.154829758385864, y: 0, size: 226, radius: 8.481629223064205 },
      { x: 5.806079662851866, y: 7.4438023223126795, size: 733, radius: 15.274853405932202 },
      { x: 9.484491297623553, y: 4.064806303558571, size: 332, radius: 10.280023453913834 },
      { x: 10.56492833796709, y: 3.0723147554880175, size: 244, radius: 8.812923024107548 },
    ];

    const area = intersectionArea(circles);
    expect(area).toBeCloseTo(10.96362);
  });
  test('2', () => {
    const circles = [
      { x: -0.0014183481763938425, y: 0.0006071174738860746, radius: 510.3115834996166 },
      { x: 875.0163281608848, y: 0.0007003612396158774, radius: 465.1793581792228 },
      { x: 462.7394999567192, y: 387.9359963330729, radius: 172.62633992134658 },
    ];
    const area = intersectionArea(circles);
    expect(area).not.toBeNaN();
  });
});
