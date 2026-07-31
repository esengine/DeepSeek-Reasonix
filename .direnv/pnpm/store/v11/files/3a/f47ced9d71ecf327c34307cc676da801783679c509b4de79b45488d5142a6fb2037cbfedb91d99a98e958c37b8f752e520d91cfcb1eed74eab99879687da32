import type { DiagramStylesProvider } from '../../diagram-api/types.js';
import { getThemeVariables } from '../../themes/theme-default.js';
import { type RailroadStyleOptions } from './railroadTypes.js';
type ThemeVariables = ReturnType<typeof getThemeVariables>;
type RailroadStyleInput = RailroadStyleOptions | ({
    railroad?: RailroadStyleOptions;
    svgId?: string;
    theme?: string;
    look?: string;
} & Partial<ThemeVariables>);
export declare const buildRailroadStyleOptions: (options?: RailroadStyleInput) => Required<RailroadStyleOptions>;
/**
 * Generate CSS styles for Railroad diagrams
 */
export declare const getStyles: DiagramStylesProvider;
export default getStyles;
