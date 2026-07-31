import { venn, lossFunction, logRatioLossFunction, normalizeSolution, scaleSolution } from './layout';
import { intersectionArea, distance, getCenter } from './circleintersection';
import { nelderMead } from 'fmin';

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
export function VennDiagram(options = {}) {
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
export function wrapText(circles, labeller) {
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
export function computeTextCentre(interior, exterior, symmetricalTextCentre) {
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

export function computeTextCentres(circles, areas, symmetricalTextCentre) {
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
export function sortAreas(div, relativeTo) {
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
export function circlePath(x, y, r) {
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
export function circleFromPath(path) {
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
export function intersectionAreaPath(circles, round) {
  return arcsToPath(intersectionAreaArcs(circles), round);
}

export function layout(data, options = {}) {
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
