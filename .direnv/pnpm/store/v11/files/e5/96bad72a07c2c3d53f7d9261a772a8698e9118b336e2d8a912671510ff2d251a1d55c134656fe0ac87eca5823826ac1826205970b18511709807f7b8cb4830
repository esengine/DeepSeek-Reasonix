var svg,
  svgSelector = "#test-inline",
  svgSelectorViewbox = "#test-viewbox",
  svgSelectorTransform = "#test-transform",
  svgSelectorViewboxTransform = "#test-viewbox-transform",
  instance;

var initSvgPanZoom = function(options, alternativeSelector) {
  if (options) {
    return svgPanZoom(alternativeSelector || svgSelector, options);
  } else {
    return svgPanZoom(alternativeSelector || svgSelector);
  }
};

/**
 * Compare numbers taking in account an error
 *
 * @param  {Float} number
 * @param  {Float} expected
 * @param  {Float} error    Optional
 * @param  {String} message  Optional
 */
var close = (QUnit.assert.close = function(number, expected, error, message) {
  if (error === void 0 || error === null) {
    error = 0.0001; // default error
  }

  /* eslint-disable eqeqeq */
  var result =
    number == expected ||
    (number < expected + error && number > expected - error) ||
    false;
  /* eslint-enable eqeqeq */

  QUnit.push(result, number, expected, message);
});

module("Test API", {
  setup: function() {},
  teardown: function() {
    instance && instance.destroy && instance.destroy();
  }
});

/**
 * Pan state (enabled, disabled)
 */

test("by default pan should be enabled", function() {
  expect(1);
  instance = initSvgPanZoom();

  equal(instance.isPanEnabled(), true);
});

test("disable pan via options", function() {
  expect(1);
  instance = initSvgPanZoom({ panEnabled: false });

  equal(instance.isPanEnabled(), false);
});

test("disable and enable pan via API", function() {
  expect(2);
  instance = initSvgPanZoom();

  instance.disablePan();
  equal(instance.isPanEnabled(), false);

  instance.enablePan();
  equal(instance.isPanEnabled(), true);
});

/**
 * Zoom state (enabled, disabled)
 */

test("by default zoom should be enabled", function() {
  expect(1);
  instance = initSvgPanZoom();

  equal(instance.isZoomEnabled(), true);
});

test("disable zoom via options", function() {
  expect(1);
  instance = initSvgPanZoom({ zoomEnabled: false });

  equal(instance.isZoomEnabled(), false);
});

test("disable and enable zoom via API", function() {
  expect(2);
  instance = initSvgPanZoom();

  instance.disableZoom();
  equal(instance.isZoomEnabled(), false);

  instance.enableZoom();
  equal(instance.isZoomEnabled(), true);
});

/**
 * Controls state (enabled, disabled)
 */

test("by default controls are disabled", function() {
  expect(1);
  instance = initSvgPanZoom();

  equal(instance.isControlIconsEnabled(), false);
});

test("enable controls via opions", function() {
  expect(1);
  instance = initSvgPanZoom({ controlIconsEnabled: true });

  equal(instance.isControlIconsEnabled(), true);
});

test("disable and enable controls via API", function() {
  expect(2);
  instance = initSvgPanZoom();

  instance.enableControlIcons();
  equal(instance.isControlIconsEnabled(), true);

  instance.disableControlIcons();
  equal(instance.isControlIconsEnabled(), false);
});

/**
 * Double click zoom state (enabled, disabled)
 */

test("by default double click zoom is enabled", function() {
  expect(1);
  instance = initSvgPanZoom();

  equal(instance.isDblClickZoomEnabled(), true);
});

test("disable double click zoom via options", function() {
  expect(1);
  instance = initSvgPanZoom({ dblClickZoomEnabled: false });

  equal(instance.isDblClickZoomEnabled(), false);
});

test("disable and enable double click zoom via API", function() {
  expect(2);
  instance = initSvgPanZoom();

  instance.disableDblClickZoom();
  equal(instance.isDblClickZoomEnabled(), false);

  instance.enableDblClickZoom();
  equal(instance.isDblClickZoomEnabled(), true);
});

/**
 * Mouse wheel zoom state (enabled, disabled)
 */

test("by default mouse wheel zoom is enabled", function() {
  expect(1);
  instance = initSvgPanZoom();

  equal(instance.isMouseWheelZoomEnabled(), true);
});

test("disable mouse wheel zoom via options", function() {
  expect(1);
  instance = initSvgPanZoom({ mouseWheelZoomEnabled: false });

  equal(instance.isMouseWheelZoomEnabled(), false);
});

test("disable and enable mouse wheel zoom via API", function() {
  expect(2);
  instance = initSvgPanZoom();

  instance.disableMouseWheelZoom();
  equal(instance.isMouseWheelZoomEnabled(), false);

  instance.enableMouseWheelZoom();
  equal(instance.isMouseWheelZoomEnabled(), true);
});

/**
 * Pan
 */

test("pan", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.pan({ x: 100, y: 300 });

  deepEqual(instance.getPan(), {
    x: 100,
    y: 300
  });
});

test("pan through API should work even if pan is disabled", function() {
  expect(1);
  instance = initSvgPanZoom({ panEnabled: false });

  instance.pan({ x: 100, y: 300 });

  deepEqual(instance.getPan(), {
    x: 100,
    y: 300
  });
});

test("pan by", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialPan = instance.getPan();

  instance.panBy({ x: 100, y: 300 });

  deepEqual(instance.getPan(), {
    x: initialPan.x + 100,
    y: initialPan.y + 300
  });
});

/**
 * Pan callbacks
 */

test("before pan", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialPan = instance.getPan();

  instance.setBeforePan(function(point) {
    deepEqual(point, initialPan);
  });

  instance.pan({ x: 100, y: 300 });

  // Remove beforePan as it will be called on destroy
  instance.setBeforePan(null);

  // Pan one more time to test if it is really removed
  instance.pan({ x: 50, y: 150 });
});

test("don't trigger on pan if canceld by before pan", function() {
  expect(1);
  instance = initSvgPanZoom({
    onPan: function() {
      QUnit.ok(true, "onUpdatedCTM got called");
    }
  });

  instance.panBy({ x: 100, y: 300 });

  instance.setBeforePan(function(oldPan, newPan) {
    return false;
  });

  instance.panBy({ x: 100, y: 300 });
});

test("don't trigger on pan if canceld by before pan for each axis separately", function() {
  expect(1);
  instance = initSvgPanZoom({
    onPan: function() {
      QUnit.ok(true, "onUpdatedCTM got called");
    }
  });

  instance.panBy({ x: 100, y: 300 });

  instance.setBeforePan(function(oldPan, newPan) {
    return { x: false, y: false };
  });

  instance.panBy({ x: 100, y: 300 });
});

test("don't trigger on pan if canceld by before pan for each axis separately", function() {
  expect(1);
  instance = initSvgPanZoom({
    onPan: function() {
      QUnit.ok(true, "onUpdatedCTM got called");
    }
  });

  instance.panBy({ x: 100, y: 300 });

  instance.setBeforePan(function(oldPan, newPan) {
    return { x: false, y: false };
  });

  instance.panBy({ x: 100, y: 300 });
});

test("on pan", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.setOnPan(function(point) {
    deepEqual(point, { x: 100, y: 300 });
  });

  instance.pan({ x: 100, y: 300 });

  // Remove onPan as it will be called on destroy
  instance.setOnPan(null);

  // Pan one more time to test if it is really removed
  instance.pan({ x: 50, y: 150 });
});

test("change only X axis when Y axis change is prevented with before pan", function() {
  expect(2);
  instance = initSvgPanZoom();
  var initialPan = instance.getPan();

  instance.setOnPan(function(newPan) {
    notEqual(newPan.x, initialPan.x);
    equal(newPan.y, initialPan.y);
  });

  instance.setBeforePan(function(oldPan, newPan) {
    return { y: false };
  });

  instance.panBy({ x: 100, y: 300 });

  // Remove onPan as it will be called on destroy
  instance.setOnPan(null);
});

test("change pan values from before pan", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.setOnPan(function(newPan) {
    deepEqual(newPan, { x: 1, y: 2 });
  });

  instance.setBeforePan(function(oldPan, newPan) {
    return { x: 1, y: 2 };
  });

  instance.panBy({ x: 100, y: 300 });

  // Remove onPan as it will be called on destroy
  instance.setOnPan(null);
});

test("don't pan if before pan makes the pan unnecessary", function() {
  expect(0);
  instance = initSvgPanZoom();
  var initialPan = instance.getPan();

  instance.setOnPan(function() {
    QUnit.ok(true, "onUpdatedCTM got called");
  });

  instance.setBeforePan(function(oldPan, newPan) {
    return { x: false, y: initialPan.y };
  });

  instance.panBy({ x: 100, y: 300 });

  // Remove onPan as it will be called on destroy
  instance.setOnPan(null);
});

/**
 * Zoom
 */

test("zoom", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.zoom(3);

  equal(instance.getZoom(), 3);
});

test("zoom by", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialZoom = instance.getZoom();

  instance.zoomBy(2);

  equal(instance.getZoom(), initialZoom * 2);
});

test("zoom at point", function() {
  expect(2);
  instance = initSvgPanZoom({ fit: false });

  instance.zoomAtPoint(2, { x: 200, y: 100 });

  close(instance.getZoom(), 2);
  deepEqual(instance.getPan(), { x: -300, y: -600 });
});

test("zoom at point by", function() {
  expect(2);
  instance = initSvgPanZoom({ fit: false });

  instance.zoomAtPointBy(2, { x: 200, y: 100 });

  close(instance.getZoom(), 2);
  deepEqual(instance.getPan(), { x: -300, y: -600 });
});

test("zoom at point by (with SVG point)", function() {
  expect(2);
  instance = initSvgPanZoom({ fit: false });

  var svgPoint = $(svgSelector)[0].createSVGPoint();
  svgPoint.x = 200;
  svgPoint.y = 100;

  instance.zoomAtPointBy(2, svgPoint);

  close(instance.getZoom(), 2);
  deepEqual(instance.getPan(), { x: -300, y: -600 });
});

test("zoom in", function() {
  expect(3);
  instance = initSvgPanZoom({ fit: false });

  instance.zoomIn();

  close(instance.getZoom(), 1.1);
  close(instance.getPan().x, -90);
  close(instance.getPan().y, -290);
});

test("zoom out", function() {
  expect(3);
  instance = initSvgPanZoom({ fit: false });

  instance.zoomOut();

  close(instance.getZoom(), 0.90909);
  close(instance.getPan().x, -13.636374);
  close(instance.getPan().y, -213.636374);
});

/**
 * Zoom settings (min, max, sensitivity)
 */

test("default min zoom", function() {
  expect(1);
  // Do not use fit as it will set original zoom different from 1
  instance = initSvgPanZoom({ fit: false });

  instance.zoom(0.1);

  equal(instance.getZoom(), 0.5);
});

test("min zoom", function() {
  expect(1);
  // Do not use fit as it will set original zoom different from 1
  instance = initSvgPanZoom({ fit: false, minZoom: 1 });

  instance.zoom(0.01);

  equal(instance.getZoom(), 1);
});

test("default max zoom", function() {
  expect(1);
  // Do not use fit as it will set original zoom different from 1
  instance = initSvgPanZoom({ fit: false });

  instance.zoom(50);

  equal(instance.getZoom(), 10);
});

test("max zoom", function() {
  expect(1);
  // Do not use fit as it will set original zoom different from 1
  instance = initSvgPanZoom({ fit: false, maxZoom: 20 });

  instance.zoom(50);

  equal(instance.getZoom(), 20);
});

test("test zoomScaleSensitivity using zoomIn and zoomOut", function() {
  expect(2);
  var sensitivity = 0.2;

  // Do not use fit as it will set original zoom different from 1
  instance = initSvgPanZoom({ fit: false, zoomScaleSensitivity: sensitivity });

  // Get initial zoom
  var initialZoom = instance.getZoom(); // should be one

  instance.zoomIn();

  close(
    instance.getZoom(),
    initialZoom * (1 + sensitivity),
    null,
    "Check if zoom in uses scale sensitivity right"
  );

  // Lets zoom to 2
  instance.zoom(2);

  // Now lets zoom out
  instance.zoomOut();

  close(
    instance.getZoom(),
    2 / (1 + sensitivity),
    null,
    "Check if zoom out uses scale sensitiviry right"
  );
});

/**
 * Zoom callbacks
 */

test("before zoom", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialZoom = instance.getZoom();

  instance.setBeforeZoom(function(scale) {
    close(scale, initialZoom);
  });

  instance.zoom(2.3);

  // Remove beforeZoom as it will be called on destroy
  instance.setBeforeZoom(null);

  // Zoom one more time to test if it is really removed
  instance.zoom(2.4);
});

test("on zoom", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.setOnZoom(function(scale) {
    close(scale, 2.3);
  });

  instance.zoom(2.3);

  // Remove onZoom as it will be called on destroy
  instance.setOnZoom(null);

  // Zoom one more time to test if it is really removed
  instance.zoom(2.4);
});

/**
 * Reseting
 */

test("reset zoom", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialZoom = instance.getZoom();

  instance.zoom(2.3);

  instance.resetZoom();

  close(instance.getZoom(), initialZoom);
});

test("reset pan", function() {
  expect(1);
  instance = initSvgPanZoom();

  var initialPan = instance.getPan();

  instance.panBy({ x: 100, y: 300 });

  instance.resetPan();

  deepEqual(instance.getPan(), initialPan);
});

test("reset (zoom and pan)", function() {
  expect(2);
  instance = initSvgPanZoom();

  var initialZoom = instance.getZoom(),
    initialPan = instance.getPan();

  instance.zoom(2.3);
  instance.panBy({ x: 100, y: 300 });

  instance.reset();

  close(instance.getZoom(), initialZoom);
  deepEqual(instance.getPan(), initialPan);
});

/**
 * Fit and center
 */

/**
 * SVG size 700x300
 * viewport zise 800x800
 *
 * If no viewBox attribute then initial zoom is always 1
 */
test("fit when initialized with fit: true", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.fit();

  close(instance.getZoom(), 1);
});

/**
 * SVG size 700x300
 * viewport zise 800x800
 * zoom = Math.min(700/800, 300/800) = 0.375
 */
test("fit when initialized with fit: false", function() {
  expect(1);
  instance = initSvgPanZoom({ fit: false, minZoom: 0.1 });

  instance.fit();

  close(instance.getZoom(), 0.375);
});

/**
 * SVG size 700x300
 * viewport zise 800x800 (sides ratio is 1)
 * zoom 1 => width = height = 300
 *
 * panX = (700 - 300)/2 = 200
 * panY = (300 - 300)/2 = 0
 */
test("center when zoom is 1", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.center();

  deepEqual(instance.getPan(), { x: 200, y: 0 });
});

/**
 * SVG size 700x300
 * viewport zise 800x800 (sides ratio is 1)
 * zoom 0.5 => width = height = 150
 *
 * panX = (700 - 150)/2 = 275
 * panY = (300 - 150)/2 = 75
 */
test("center when zoom is 0.5", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.zoom(0.5);
  instance.center();

  deepEqual(instance.getPan(), { x: 275, y: 75 });
});

/**
 * Resize
 */

// TODO resize

/**
 * On updated CTM callback
 */

asyncTest("onUpdatedCTM is called", function() {
  // onUpdatedCTM will get called once on init and once after panBy
  expect(2);

  instance = initSvgPanZoom();
  instance.setOnUpdatedCTM(function() {
    QUnit.ok(true, "onUpdatedCTM got called");
  });
  instance.panBy({ x: 100, y: 300 });

  setTimeout(function() {
    start();
  }, 100);
});

/**
 * Destroy
 */

test("after destroy calling svgPanZoom again should return a new instance", function() {
  expect(1);
  instance = initSvgPanZoom();

  instance.destroy();

  var instance2 = initSvgPanZoom();

  notStrictEqual(instance2, instance);

  // Set it as null so teardown will not try to destroy it again
  instance = null;

  // Destroy second instance
  instance2.destroy();
  instance2 = null;
});
