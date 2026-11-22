// The world map: the light world, dark world or underworld map of the loaded ROM, with lines between its areas or
// rooms, the areas and rooms the players are in outlined, the area and room numbers around it, and a dot for each
// player. A port of alttpo's WorldMapWindow.
import {useEffect, useState} from "preact/hooks";
import {Fragment} from "preact";
import {ViewModel} from "../viewmodel";

// a player's position, from the "game/positions" view model:
export interface PlayerPosition {
    index: number;
    name: string;
    color: number;
    local: boolean;
    module: number;
    location: number;
    x: number;
    y: number;
    lastOverworldX: number;
    lastOverworldY: number;
}

const enum Screen {
    LightWorld = 0,
    DarkWorld = 1,
    Underworld = 2,
}

const screenNames = ["Light World", "Dark World", "Underworld"];

// localStorage keys of the map's Auto checkbox and the map picked while it is off:
const autoKey = "o2.alttp.map.auto";
const screenKey = "o2.alttp.map.screen";

// the size of an overworld map image in pixels, which are the units the overworld maps are drawn in:
const owWidth = 48 * 8;
const owHeight = 57 * 8;
// the map image starts this many pixels into the Mode 7 tilemap:
const owLeft = 8 * 8;
const owTop = 4 * 8;
// added to an overworld coordinate before it is divided by 16 to place it on the map:
const owSprLeft = 2048;
const owSprTop = 2132;

// the underworld map is drawn in the game's own coordinates: 16 columns and 18 rows of rooms of 512x512 pixels:
const uwCols = 16;
const uwRows = 18;
const uwRoom = 512;
const uwWidth = uwCols * uwRoom;
const uwHeight = uwRows * uwRoom;
// the underworld map image is made 4 times smaller than full size:
const uwScale = 4;

// the lines between areas and rooms are half as bright as the brightest of the maps, and the lines around the
// players' areas and rooms brighter than it (alttpo's mapMaxBrightness of 140); the underworld map image is darkened
// to about as bright as the overworld maps:
const gridColor = "rgb(66, 66, 66)";
const highlightColor = "rgb(247, 247, 247)";
const axisColor = "rgb(140, 140, 140)";
const underworldFilter = "brightness(0.6)";

interface Geometry {
    width: number;
    height: number;
    cols: number;
    rows: number;
    // where the first column and row start and the size of each:
    x0: number;
    y0: number;
    cell: number;
}

function geometryFor(screen: Screen): Geometry {
    if (screen === Screen.Underworld) {
        return {width: uwWidth, height: uwHeight, cols: uwCols, rows: uwRows, x0: 0, y0: 0, cell: uwRoom};
    }
    return {
        width: owWidth,
        height: owHeight,
        cols: 8,
        rows: 8,
        x0: owSprLeft / 16 - owLeft,
        y0: owSprTop / 16 - owTop,
        cell: 512 / 16,
    };
}

// which screens of the overworld are part of a large area, which is 2x2 screens as one; from the table
// OverworldScreenSizeFlag in bank $02. The light world and the dark world are laid out the same:
const owLarge = [
    1, 1, 0, 1, 1, 1, 1, 0,
    1, 1, 0, 1, 1, 1, 1, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    1, 1, 0, 1, 1, 0, 1, 1,
    1, 1, 0, 1, 1, 0, 1, 1,
    0, 0, 0, 0, 0, 0, 0, 0,
    1, 1, 0, 0, 0, 1, 1, 0,
    1, 1, 0, 0, 0, 1, 1, 0,
];

// owBlock is the same for all the screens of one area: a small area is its own screen and a large area is its top
// left screen; large areas are in pairs of rows 0-1, 3-4 and 6-7 and in pairs of columns within each run of columns:
function owBlock(r: number, c: number): number {
    if (owLarge[r * 8 + c] === 0) return r * 8 + c;
    const top = Math.floor(r / 3) * 3;
    let start = c;
    while (start > 0 && owLarge[r * 8 + start - 1] !== 0) start--;
    const left = start + ((c - start) & ~1);
    return top * 8 + left;
}

function hex(value: number, digits: number): string {
    return value.toString(16).toUpperCase().padStart(digits, "0");
}

// gridPath is an SVG path of the lines between the rooms or areas; areas within a large area have none:
function gridPath(screen: Screen, g: Geometry): string {
    const d: string[] = [];
    const {cols, rows, x0, y0, cell} = g;

    // lines between columns, in segments of rows:
    for (let c = 0; c <= cols; c++) {
        let segStart = -1;
        for (let r = 0; r <= rows; r++) {
            let draw = false;
            if (r < rows) {
                draw = screen === Screen.Underworld || c === 0 || c === cols || owBlock(r, c - 1) !== owBlock(r, c);
            }
            if (draw && segStart < 0) segStart = r;
            if (!draw && segStart >= 0) {
                d.push(`M${x0 + cell * c} ${y0 + cell * segStart}V${y0 + cell * r}`);
                segStart = -1;
            }
        }
    }

    // lines between rows, in segments of columns:
    for (let r = 0; r <= rows; r++) {
        let segStart = -1;
        for (let c = 0; c <= cols; c++) {
            let draw = false;
            if (c < cols) {
                draw = screen === Screen.Underworld || r === 0 || r === rows || owBlock(r - 1, c) !== owBlock(r, c);
            }
            if (draw && segStart < 0) segStart = c;
            if (!draw && segStart >= 0) {
                d.push(`M${x0 + cell * segStart} ${y0 + cell * r}H${x0 + cell * c}`);
                segStart = -1;
            }
        }
    }

    return d.join("");
}

// inUnderworldMap tells if the player is in a room that is on the underworld map; the rooms of dungeons, caves and
// houses are all below $120, including the rooms from $100 up (e.g. Link's house is room $104):
function inUnderworldMap(p: PlayerPosition): boolean {
    return (p.location & 0x010000) !== 0 && (p.location & 0xFFFF) < uwCols * uwRows;
}

// inGame tells if the player is in a module of the game itself, and not at the title screen, file select, intro or
// end of the game, where they have no position:
function inGame(p: PlayerPosition): boolean {
    if (p.module <= 0x05) return false;
    if (p.module === 0x14) return false;
    if (p.module >= 0x1B) return false;
    return true;
}

function screenFor(p: PlayerPosition): Screen {
    if (inUnderworldMap(p)) {
        // dungeons, and caves and houses too, which are the rooms of the underworld as well:
        return Screen.Underworld;
    }
    return (p.location & 0x020000) !== 0 ? Screen.DarkWorld : Screen.LightWorld;
}

function showsOnScreen(p: PlayerPosition, screen: Screen): boolean {
    switch (screen) {
        case Screen.LightWorld:
            return (p.location & 0x020000) === 0;
        case Screen.DarkWorld:
            return (p.location & 0x020000) !== 0;
        case Screen.Underworld:
            return inUnderworldMap(p);
    }
    return false;
}

// mapCoord is where the player is on the map, in the map's units:
function mapCoord(p: PlayerPosition, screen: Screen): [number, number] {
    if (screen === Screen.Underworld) {
        let px = p.x;
        let py = p.y;
        // during glitched states (e.g. Kiki skip), the Y coordinate can exceed $2000 and the dot would show in the
        // rooms from $100 up, so it wraps:
        if ((p.location & 0x010000) !== 0) {
            if ((p.location & 0xFFFF) < 0x100) {
                py &= 0x1FFF;
            } else if (py >= uwHeight) {
                py = uwHeight;
            }
        }
        // always wrap the X coordinate:
        px &= 0x1FFF;
        return [px, py];
    }

    // where the player last was on the overworld while in a cave or house or dungeon:
    let px = p.lastOverworldX;
    let py = p.lastOverworldY;
    if (p.module === 0x09 || p.module === 0x0B || p.module === 0x10) {
        px = p.x;
        py = p.y;

        if (p.location === 0x000080) {
            if (px < 0x100) {
                // in the master sword grove:
                px += 0x20;
                py += -0x146;
            } else if (px < 0x200) {
                // under the bridge:
                px = Math.trunc((px - 0x108) * 0x40 / 0xe8) + 0xaf0;
                py = Math.trunc((py - 0x018) * 0x50 / 0xa0) + 0xaf0;
            }
        } else if (p.location === 0x000081) {
            // in zora's waterfall:
            px = 0xf50;
            py = 0x213;
        }

        // wrap the coordinates on the overworld:
        px &= 0xfff;
        py &= 0xfff;
    }

    return [(px + owSprLeft) / 16 - owLeft, (py + owSprTop) / 16 - owTop];
}

// highlightRects are the rooms or areas the players shown are in, as [c0, r0, c1, r1]; a large area is one rect:
function highlightRects(players: PlayerPosition[], screen: Screen): number[][] {
    const rects: number[][] = [];
    for (const p of players) {
        if (!inGame(p) || !showsOnScreen(p, screen)) continue;

        let c0: number, r0: number, c1: number, r1: number;
        if (screen === Screen.Underworld) {
            const room = p.location & 0xFFFF;
            c0 = c1 = room & 15;
            r0 = r1 = room >> 4;
        } else {
            // overworld areas only, not the caves and houses:
            if ((p.location & 0x010000) !== 0) continue;
            let area = p.location & 0xFF;
            if (area >= 0x80) continue;
            area &= 0x3F;
            const r = area >> 3;
            const c = area & 7;
            c0 = c1 = c;
            r0 = r1 = r;
            // the extent of the large area:
            const block = owBlock(r, c);
            for (let rr = 0; rr < 8; rr++) {
                for (let cc = 0; cc < 8; cc++) {
                    if (owBlock(rr, cc) !== block) continue;
                    c0 = Math.min(c0, cc);
                    c1 = Math.max(c1, cc);
                    r0 = Math.min(r0, rr);
                    r1 = Math.max(r1, rr);
                }
            }
        }

        // players in the same room share the lines:
        if (rects.some(x => x[0] === c0 && x[1] === r0 && x[2] === c1 && x[3] === r1)) continue;
        rects.push([c0, r0, c1, r1]);
    }
    return rects;
}

// locationText is the room (underworld) or area (overworld) the local player is in, if it is on the map shown:
function locationText(p: PlayerPosition | undefined, screen: Screen): string {
    if (!p || !inGame(p)) return "";
    if (screen === Screen.Underworld && showsOnScreen(p, screen)) {
        return "$" + hex(p.location & 0xFFFF, 3);
    }
    if (screen !== Screen.Underworld && showsOnScreen(p, screen) && (p.location & 0x010000) === 0) {
        return "$" + hex(p.location & 0xFF, 2);
    }
    return "";
}

// the CSS color of a SNES BGR555 color, scaled by `scale`:
function cssColor(bgr: number, scale = 1): string {
    const c5 = (v: number) => Math.floor(v * scale);
    const c8 = (v: number) => (v << 3) | (v >> 2);
    const r = c8(c5(bgr & 31));
    const g = c8(c5((bgr >> 5) & 31));
    const b = c8(c5((bgr >> 10) & 31));
    return `rgb(${r}, ${g}, ${b})`;
}

function loadSetting(key: string, def: string): string {
    try {
        return window.localStorage.getItem(key) ?? def;
    } catch {
        return def;
    }
}

function saveSetting(key: string, value: string) {
    try {
        window.localStorage.setItem(key, value);
    } catch {
        // the setting is only a convenience
    }
}

export function WorldMap({vm}: { vm: ViewModel }) {
    const players = (vm["game/positions"] || []) as PlayerPosition[];
    const local = players.find(p => p.local);

    const [auto, setAuto] = useState(() => loadSetting(autoKey, "1") === "1");
    const [picked, setPicked] = useState(() => (parseInt(loadSetting(screenKey, "0"), 10) || 0) as Screen);
    // the screen the Auto checkbox last followed the local player to:
    const [followed, setFollowed] = useState(Screen.LightWorld);
    useEffect(() => {
        if (local && inGame(local)) {
            setFollowed(screenFor(local));
        }
    }, [local?.location, local?.module]);
    const screen = auto ? followed : picked;

    // the map images, reloaded when another ROM is loaded; the images that failed to load:
    const romKey = vm.rom?.isLoaded ? encodeURIComponent(vm.rom.name || "") : "";
    const [failed, setFailed] = useState({} as { [url: string]: boolean });
    const urls = [
        `/rom/lightworld.png?r=${romKey}`,
        `/rom/darkworld.png?r=${romKey}`,
        `/rom/underworld.png?scale=${uwScale}&r=${romKey}`,
    ];
    useEffect(() => setFailed({}), [romKey]);

    const g = geometryFor(screen);
    const rects = highlightRects(players, screen);
    const loc = locationText(local, screen);

    const axisTop: { text: string, at: number }[] = [];
    for (let c = 0; c < g.cols; c++) {
        axisTop.push({
            text: screen === Screen.Underworld ? "$xx" + hex(c, 1) : "$x" + hex(c, 1),
            at: (g.x0 + g.cell * (c + 0.5)) / g.width * 100,
        });
    }
    const axisLeft: { text: string, at: number }[] = [];
    for (let r = 0; r < g.rows; r++) {
        axisLeft.push({
            // the underworld's rooms of the row, or the first area of the row; the dark world's areas are $40 and up:
            text: screen === Screen.Underworld
                ? "$" + hex(r, 2) + "x"
                : "$" + hex(r * 8 + (screen === Screen.DarkWorld ? 0x40 : 0), 2),
            at: (g.y0 + g.cell * (r + 0.5)) / g.height * 100,
        });
    }

    return <div class="world-map">
        <div class="world-map-controls">
            <select value={screen}
                    disabled={auto}
                    onChange={e => {
                        const s = parseInt((e.target as HTMLSelectElement).value, 10) as Screen;
                        setPicked(s);
                        saveSetting(screenKey, s.toString());
                    }}>
                {screenNames.map((name, i) => <option value={i}>{name}</option>)}
            </select>
            <span class="world-map-location mono"
                  title="The room (underworld) or area (overworld) you are in">{loc}</span>
            <label title="Show the map you are on">
                <input type="checkbox"
                       checked={auto}
                       onChange={e => {
                           const checked = (e.target as HTMLInputElement).checked;
                           if (!checked) {
                               // keep showing the map it followed to:
                               setPicked(screen);
                               saveSetting(screenKey, screen.toString());
                           }
                           setAuto(checked);
                           saveSetting(autoKey, checked ? "1" : "0");
                       }}/>Auto
            </label>
        </div>
        {!romKey ? <div class="world-map-empty">Load a ROM to see the map.</div> : (
            <div class="world-map-axes">
                <div class="world-map-frame"
                     style={`aspect-ratio: ${g.width} / ${g.height}; width: min(100%, calc(80vh * ${g.width} / ${g.height}))`}>
                    {urls.map((url, i) => (
                        <img key={i}
                             src={url}
                             alt=""
                             class={i === Screen.Underworld ? "" : "pixelated"}
                             style={`display: ${i === screen && !failed[url] ? "block" : "none"}` +
                                 (i === Screen.Underworld ? `; filter: ${underworldFilter}` : "")}
                             onError={() => setFailed(f => ({...f, [url]: true}))}
                             onLoad={() => setFailed(f => (f[url] ? {...f, [url]: false} : f))}/>
                    ))}
                    {failed[urls[screen]] && <div class="world-map-empty">The map could not be drawn from this ROM.</div>}
                    <svg viewBox={`0 0 ${g.width} ${g.height}`} preserveAspectRatio="none">
                        <path d={gridPath(screen, g)} stroke={gridColor} stroke-width={1} fill="none"
                              vector-effect="non-scaling-stroke" shape-rendering="crispEdges"/>
                        {rects.map(([c0, r0, c1, r1]) => (
                            <rect x={g.x0 + g.cell * c0} y={g.y0 + g.cell * r0}
                                  width={g.cell * (c1 - c0 + 1)} height={g.cell * (r1 - r0 + 1)}
                                  stroke={highlightColor} stroke-width={1} fill="none"
                                  vector-effect="non-scaling-stroke" shape-rendering="crispEdges"/>
                        ))}
                    </svg>
                    {axisTop.map(a => (<Fragment key={"c" + a.text}>
                        <div class="world-map-axis world-map-axis-top" style={`left: ${a.at}%; color: ${axisColor}`}>{a.text}</div>
                        <div class="world-map-axis world-map-axis-bottom" style={`left: ${a.at}%; color: ${axisColor}`}>{a.text}</div>
                    </Fragment>))}
                    {axisLeft.map(a => (<Fragment key={"r" + a.text}>
                        <div class="world-map-axis world-map-axis-left" style={`top: ${a.at}%; color: ${axisColor}`}>{a.text}</div>
                        <div class="world-map-axis world-map-axis-right" style={`top: ${a.at}%; color: ${axisColor}`}>{a.text}</div>
                    </Fragment>))}
                    {players.filter(p => inGame(p) && showsOnScreen(p, screen)).map(p => {
                        const [x, y] = mapCoord(p, screen);
                        // keyed by the screen too, so that a dot doesn't glide across the map when the map changes:
                        return <div key={screen + ":" + p.index}
                                    class={"world-map-dot" + (p.local ? " world-map-dot-local" : "")}
                                    title={p.name}
                                    style={`left: ${x / g.width * 100}%; top: ${y / g.height * 100}%; ` +
                                        `background-color: ${cssColor(p.color)}; border-color: ${cssColor(p.color, 0.75)}`}/>;
                    })}
                </div>
            </div>
        )}
    </div>;
}
