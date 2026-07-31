import { computeTextCentre } from './diagram';
import { describe, test, expect } from 'vitest';

describe('computeTextCentre', () => {
  test('0', () => {
    const center = computeTextCentre([{ x: 0, y: 0, radius: 1 }], []);
    expect(center.x).toBeCloseTo(0);
    expect(center.y).toBeCloseTo(0);
  });

  test('1', () => {
    const center = computeTextCentre([{ x: 0, y: 0, radius: 1 }], [{ x: 0, y: 1, radius: 1 }]);
    expect(center.x).toBeCloseTo(0, 4);
    expect(center.y).toBeCloseTo(-0.5);
  });
});
