import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { classifySeverity } from './incidents.js';

describe('classifySeverity', () => {
  test('returns "low" for scores less than 40', () => {
    assert.equal(classifySeverity(0), 'low');
    assert.equal(classifySeverity(10), 'low');
    assert.equal(classifySeverity(39), 'low');
    assert.equal(classifySeverity(39.9), 'low');
  });

  test('returns "medium" for scores between 40 and 59', () => {
    assert.equal(classifySeverity(40), 'medium');
    assert.equal(classifySeverity(50), 'medium');
    assert.equal(classifySeverity(59), 'medium');
    assert.equal(classifySeverity(59.9), 'medium');
  });

  test('returns "high" for scores between 60 and 79', () => {
    assert.equal(classifySeverity(60), 'high');
    assert.equal(classifySeverity(70), 'high');
    assert.equal(classifySeverity(79), 'high');
    assert.equal(classifySeverity(79.9), 'high');
  });

  test('returns "critical" for scores 80 and above', () => {
    assert.equal(classifySeverity(80), 'critical');
    assert.equal(classifySeverity(90), 'critical');
    assert.equal(classifySeverity(100), 'critical');
    assert.equal(classifySeverity(150), 'critical');
  });

  test('handles edge cases correctly', () => {
    assert.equal(classifySeverity(-10), 'low');
    assert.equal(classifySeverity(Infinity), 'critical');
  });
});
