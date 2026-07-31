import { disjointCluster, normalizeSolution, greedyLayout, lossFunction, distanceFromIntersectArea } from './layout';
import { distance, circleOverlap } from './circleintersection';
import { describe, test, expect } from 'vitest';

describe('greedyLayout', () => {
  test('0', () => {
    const areas = [
      { sets: [0], size: 0.7746543297103429 },
      { sets: [1], size: 0.1311252856844238 },
      { sets: [2], size: 0.2659942131443344 },
      { sets: [3], size: 0.44600866168641723 },
      { sets: [0, 1], size: 0.02051532092950205 },
      { sets: [0, 2], size: 0 },
      { sets: [0, 3], size: 0 },
      { sets: [1, 2], size: 0 },
      { sets: [1, 3], size: 0.07597023820511245 },
      { sets: [2, 3], size: 0 },
    ];
    const circles = greedyLayout(areas);
    const loss = lossFunction(circles, areas);
    expect(loss).toBeCloseTo(0);
  });

  test('1', () => {
    const areas = [
      { sets: [0], size: 0.5299368855059736 },
      { sets: [1], size: 0.03364187025606481 },
      { sets: [2], size: 0.3121450394871512 },
      { sets: [3], size: 0.0514397361783036 },
      { sets: [0, 1], size: 0.013912447645582351 },
      { sets: [0, 2], size: 0.005903647141469598 },
      { sets: [0, 3], size: 0.0514397361783036 },
      { sets: [1, 2], size: 0.012138157839477597 },
      { sets: [1, 3], size: 0.008010688232481479 },
      { sets: [2, 3], size: 0 },
    ];

    const circles = greedyLayout(areas);
    const loss = lossFunction(circles, areas);
    expect(loss).toBeCloseTo(0);
  });

  test('3', () => {
    // one small circle completely overlapped in the intersection
    // area of two larger circles
    const areas = [
      { sets: [0], size: 1.7288584050841396 },
      { sets: [1], size: 0.040875831658950056 },
      { sets: [2], size: 2.587146019782323 },
      { sets: [0, 1], size: 0.040875831658950056 },
      { sets: [0, 2], size: 0.5114617575187569 },
      { sets: [1, 2], size: 0.040875831658950056 },
    ];

    const circles = greedyLayout(areas);
    const loss = lossFunction(circles, areas);
    expect(loss).toBeCloseTo(0);
  });
});

test('distanceFromIntersectArea', () => {
  function testDistanceFromIntersectArea(r1, r2, overlap) {
    const distance = distanceFromIntersectArea(r1, r2, overlap);
    expect(circleOverlap(r1, r2, distance)).toBeCloseTo(overlap);
  }

  testDistanceFromIntersectArea(1.9544100476116797, 2.256758334191025, 11);

  testDistanceFromIntersectArea(111.06512962798197, 113.32348546565727, 1218);

  testDistanceFromIntersectArea(44.456564007075, 149.4335753619362, 2799);

  testDistanceFromIntersectArea(592.89, 134.75, 56995);

  testDistanceFromIntersectArea(139.50778247443944, 32.892784970851956, 3399);

  testDistanceFromIntersectArea(4.886025119029199, 5.077706251929807, 75);
});

test('normalizeSolution', () => {
  // test two circles that are far apart
  const solution = [
    { x: 0, y: 0, radius: 0.5 },
    { x: 1e10, y: 0, radius: 1.5 },
  ];

  // should be placed close together
  const normalized = normalizeSolution(solution);
  // distance should be 2, but we space things out
  expect(distance(normalized[0], normalized[1])).toBeLessThan(2.1);
});

test('disjointClusters', () => {
  const input = [
    {
      x: 0.8047033110633492,
      y: 0.9396705999970436,
      radius: 0.47156485118903224,
    },
    {
      x: 0.7961132447235286,
      y: 0.014027722179889679,
      radius: 0.14554832570720466,
    },
    {
      x: 0.28841276094317436,
      y: 0.98081015329808,
      radius: 0.9851036085514352,
    },
    {
      x: 0.7689983483869582,
      y: 0.2899463507346809,
      radius: 0.7210563338827342,
    },
  ];

  const clusters = disjointCluster(input);
  expect(clusters).toHaveLength(1);
});
