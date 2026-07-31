import type { Diagram } from '../../Diagram.js';
import type { MermaidConfig } from '../../config.type.js';
import type { SVG } from '../../diagram-api/types.js';
interface TimelineTask {
    id: number;
    section: string;
    type: string;
    task: string;
    score: number;
    events: string[];
}
export declare const draw: (text: string, id: string, version: string, diagObj: Diagram) => void;
export declare const drawTasks: (diagram: SVG, tasks: TimelineTask[], sectionColor: number, timelineX: number, masterY: number, maxTaskHeight: number, conf: MermaidConfig, taskSpacing: number, isWithoutSections: boolean) => void;
export declare const drawEvents: (diagram: SVG, events: string[], sectionColor: number, axisX: number, eventsX: number, startY: number, conf: MermaidConfig) => number;
declare const _default: {
    setConf: () => void;
    draw: (text: string, id: string, version: string, diagObj: Diagram) => void;
};
export default _default;
