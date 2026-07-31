# venn.js

[![License: MIT][mit-image]][mit-url] [![NPM Package][npm-image]][npm-url] [![Github Actions][github-actions-image]][github-actions-url]

This is a maintained fork of [https://github.com/benfred/venn.js](https://github.com/benfred/venn.js).

A javascript library for laying out area proportional venn and euler diagrams.

Details of how this library works can be found on the [blog
post](https://www.benfrederickson.com/venn-diagrams-with-d3.js/)
the original author wrote about this. A follow up post [discusses testing strategy and
algorithmic improvements](https://www.benfrederickson.com/better-venn-diagrams/).

## Install

```bash
npm install --save @upsetjs/venn.js
```

## Usage

There are two modes in which this library can be used.
First, in a managed case by using the `VennDiagram` function that will render the data using D3.
Second, in a manual case as a layout library that is just preparing the data for you.

In the following, these set data are used:

```js
const sets = [
  { sets: ['A'], size: 12 },
  { sets: ['B'], size: 12 },
  { sets: ['A', 'B'], size: 2 },
];
```

### Managed Usage

This library depends on [d3.js](https://d3js.org/) to display the venn
diagrams.

##### Simple layout

To lay out a simple diagram, just define the sets and their sizes along with the sizes
of all the set intersections.

The VennDiagram object will calculate a layout that is proportional to the
input sizes, and display it in the appropriate selection when called:

```js
const chart = venn.VennDiagram();
d3.select('#venn').datum(sets).call(chart);
```

[View this example](https://upset.js.org/venn.js/examples/simple.html)

[![Open in CodePen][codepen]](https://codepen.io/sgratzl/pen/RwrKPEe)

##### Changing the Style

The style of the Venn Diagram can be customized by using D3 after the diagram
has been drawn. For instance to draw a Venn Diagram with white text and a darker fill:

```js
const chart = venn.VennDiagram();
d3.select('#inverted').datum(sets).call(chart);

d3.selectAll('#inverted .venn-circle path').style('fill-opacity', 0.8);

d3.selectAll('#inverted text').style('fill', 'white');
```

[View this example, along with other possible styles](https://upset.js.org/venn.js/examples/styled.html)

The position of text within each circle of the diagram may also be modified via the `symmetricalTextCentre` property (defaults to `false`):

```js
// draw a diagram with text symmetrically positioned in each circle's centre
const chart = venn.VennDiagram({ symmetricalTextCentre: true });
```

##### Dynamic layout

To have a layout that reacts to a change in input, all that you need to do is
update the dataset and call the chart again:

```js
// draw the initial diagram
const chart = venn.VennDiagram();
d3.select('#venn').datum(getSetIntersections()).call(chart);

// redraw the diagram on any change in input
d3.selectAll('input').on('change', function () {
  d3.select('#venn').datum(getSetIntersections()).call(chart);
});
```

[View this example](https://upset.js.org/venn.js/examples/dynamic.html)

##### Making the diagram interactive

Making the diagram interactive is basically the same idea as changing the style: just add event listeners to the elements in the venn diagram. To change the text size and circle colours on mouseenter:

```js
d3.selectAll('#rings .venn-circle')
  .on('mouseenter', function () {
    const node = d3.select(this).transition();
    node.select('path').style('fill-opacity', 0.2);
    node.select('text').style('font-weight', '100').style('font-size', '36px');
  })
  .on('mouseleave', function () {
    const node = d3.select(this).transition();
    node.select('path').style('fill-opacity', 0);
    node.select('text').style('font-weight', '100').style('font-size', '24px');
  });
```

[View this example](https://upset.js.org/venn.js/examples/interactive.html)

The colour scheme for the diagram's circles may also be modified via the `colorScheme` option, and the text within each circle can have its fill modified via the `textFill` option:

```js
const chart = venn.VennDiagram({
  colorScheme: ['rgb(235, 237, 238)', '#F26250'],
  textFill: '#FFF',
});
```

##### Adding tooltips

Another common case is adding a tooltip when hovering over the elements in the diagram. The only
tricky thing here is maintaining the correct Z-order so that the smallest intersection areas
are on top, while still making the area that is being hovered over appear on top of the others:

```js
// draw venn diagram
const div = d3.select('#venn');
div.datum(sets).call(venn.VennDiagram());

// add a tooltip
const tooltip = d3.select('body').append('div').attr('class', 'venntooltip');

// add listeners to all the groups to display tooltip on mouseenter
div
  .selectAll('g')
  .on('mouseenter', function (d) {
    // sort all the areas relative to the current item
    venn.sortAreas(div, d);

    // Display a tooltip with the current size
    tooltip.transition().duration(400).style('opacity', 0.9);
    tooltip.text(d.size + ' users');

    // highlight the current path
    const selection = d3.select(this).transition('tooltip').duration(400);
    selection
      .select('path')
      .style('stroke-width', 3)
      .style('fill-opacity', d.sets.length == 1 ? 0.4 : 0.1)
      .style('stroke-opacity', 1);
  })

  .on('mousemove', function () {
    tooltip.style('left', d3.event.pageX + 'px').style('top', d3.event.pageY - 28 + 'px');
  })

  .on('mouseleave', function (d) {
    tooltip.transition().duration(400).style('opacity', 0);
    const selection = d3.select(this).transition('tooltip').duration(400);
    selection
      .select('path')
      .style('stroke-width', 0)
      .style('fill-opacity', d.sets.length == 1 ? 0.25 : 0.0)
      .style('stroke-opacity', 0);
  });
```

[View this example](https://upset.js.org/venn.js/examples/intersection_tooltip.html)

## Manual Usage

Besides the handy `VennDiagram` wrapper, the library can used as a pure layout function using the `layout` method.
One can render the result manually in D3 or even in HTML Canvas.

The signature of the function can be found as part of the TypeScript typings at [index.ds.ts](https://github.com/upsetjs/venn.js/blob/master/src/index.d.ts)

### Custom D3 Rendering

```js
// compute layout data
const data = venn.layout(sets);
// custom data binding and rendering
const g = d3
  .select('#venn')
  .selectAll('g')
  .data(data)
  .join((enter) => {
    const g = enter.append('g');
    g.append('title');
    g.append('path');
    g.append('text');
    return g;
  });
g.select('title').text((d) => d.data.sets.toString());
g.select('text')
  .text((d) => d.data.sets.toString())
  .attr('x', (d) => d.text.x)
  .attr('y', (d) => d.text.y);
g.select('path')
  .attr('d', (d) => d.path)
  .style('fill', (d, i) => (d.circles.length === 1 ? d3.schemeCategory10[i] : undefined));
```

[![Open in CodePen][codepen]](https://codepen.io/sgratzl/pen/xxZgGeP)

### Canvas Rendering

```js
const data = venn.layout(sets, { width: 600, height: 350 });
const ctx = document.querySelector('canvas').getContext('2d');

data.forEach((d, i) => {
  ctx.fillStyle = `hsla(${(360 * i) / data.length},80%,50%,0.6)`;
  ctx.fill(new Path2D(d.path));
});

ctx.font = '16px Helvetica Neue, Helvetica, Arial, sans-serif';
ctx.textAlign = 'center';
ctx.textBaseline = 'central';
ctx.fillStyle = 'white';

data.forEach((d, i) => {
  ctx.fillText(d.data.sets.toString(), d.text.x, d.text.y);
});
```

[![Open in CodePen][codepen]](https://codepen.io/sgratzl/pen/NWxdqZW)

## License

Released under the MIT License.

## Development Environment

```sh
npm i -g yarn
yarn install
yarn sdks vscode
```

### Common commands

```sh
yarn test
yarn lint
yarn format
yarn build
yarn release
yarn release:pre
```

[mit-image]: https://img.shields.io/badge/License-MIT-yellow.svg
[mit-url]: https://opensource.org/licenses/MIT
[npm-image]: https://badge.fury.io/js/%40upsetjs%2Fvenn.js.svg
[npm-url]: https://npmjs.org/package/@upsetjs/venn.js
[github-actions-image]: https://github.com/upsetjs/venn.js/workflows/ci/badge.svg
[github-actions-url]: https://github.com/upsetjs/venn.js/actions
[codepen]: https://img.shields.io/badge/CodePen-open-blue?logo=codepen
