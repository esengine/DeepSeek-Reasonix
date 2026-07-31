import type { DiagramDB } from '../../diagram-api/types.js';
import type { IshikawaNode } from './ishikawaTypes.js';
export declare class IshikawaDB implements DiagramDB {
    private root?;
    private stack;
    private baseLevel?;
    constructor();
    clear(): void;
    getRoot(): IshikawaNode | undefined;
    addNode(rawLevel: number, text: string): void;
    getAccTitle(): string;
    setAccTitle(title: string): void;
    getAccDescription(): string;
    setAccDescription(description: string): void;
    getDiagramTitle(): string;
    setDiagramTitle(title: string): void;
}
