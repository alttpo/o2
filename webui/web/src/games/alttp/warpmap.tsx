// The underworld map's rooms and the overworld's areas as the backend draws them, and picking a point of a room or an
// area to warp Link to in debug mode.
//
// The backend draws the rooms of the loaded ROM one by one (see underworldmap/stream.go), and the overworld's areas
// (underworldmap/overworldstream.go), and tells which ones are ready over server-sent events (/rom/underworld/events,
// /rom/overworld/events); the underworld map shows each room as soon as it is. In debug mode, hovering a room or an
// area shows a zoomed-in view of it under the cursor with where Link would be placed: the nearest point where he can
// stand that is reachable from its ways in (or any point where he can stand, if it has none), from its walk cells
// (/rom/underworld/walk.json, /rom/overworld/walk.json; see underworldmap/walk.go and overworldwalk.go). Clicking pins
// that point as the target of the Warp section's "Warp to map point", and double-clicking warps Link there right
// away.
import {useEffect, useRef, useState} from "preact/hooks";
import {placeNames} from "./roomnames";

// the walk cell flags (underworldmap.Walk*):
const WalkStandable = 1 << 0;
const WalkReachable = 1 << 1;
const WalkPassable = 1 << 2;
const WalkHazard = 1 << 3;
const WalkLower = 1 << 4;

export const roomPixels = 512;

// Place is a room of the underworld or an area of the overworld (a parent area: a small one or the top left screen of
// a large one).
export interface Place {
    kind: "room" | "area";
    id: number;
}

// WarpTarget is a point of an underworld room or an overworld area where Link can be placed.
export type WarpTarget = RoomTarget | AreaTarget;

// RoomTarget is a point of an underworld room: Link's coordinates in the room's pixels, as the game keeps them ($22,
// $20), and the layer ($EE).
export interface RoomTarget {
    kind: "room";
    room: number;
    x: number;
    y: number;
    layer: number;
    // whether the point is reachable from the room's ways in:
    reachable: boolean;
    // whether Link is on the lower level there ($0476), which isn't always the layer:
    lower: boolean;
    // the dungeon ($040C) the room is in, $FF for caves and houses:
    palace: number;
}

// AreaTarget is a point of an overworld area: Link's coordinates in the area's pixels (the overworld's, less the
// area's origin).
export interface AreaTarget {
    kind: "area";
    area: number;
    x: number;
    y: number;
    reachable: boolean;
}

export function targetPlace(t: WarpTarget): Place {
    return t.kind === "room" ? {kind: "room", id: t.room} : {kind: "area", id: t.area};
}

// warpOp is the debug op that warps Link to the target.
export function warpOp(t: WarpTarget) {
    if (t.kind === "area") return {t: "warp", w: "area", r: t.area, x: t.x, y: t.y} as const;
    return {t: "warp", w: "room", r: t.room, x: t.x, y: t.y, l: t.layer} as const;
}

// the size of a small overworld area in pixels; a large one is twice as wide and tall:
export const areaPixels = 512;

// which screens of the overworld are part of a large area, which is 2x2 screens as one; from the table
// OverworldScreenSizeFlag in bank $02. The light world and the dark world are laid out the same:
export const owLarge = [
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
export function owBlock(r: number, c: number): number {
    if (owLarge[r * 8 + c] === 0) return r * 8 + c;
    const top = Math.floor(r / 3) * 3;
    let start = c;
    while (start > 0 && owLarge[r * 8 + start - 1] !== 0) start--;
    const left = start + ((c - start) & ~1);
    return top * 8 + left;
}

export interface MapStream {
    // the map's id, for the URLs of its rooms or areas; 0 until the backend has said:
    id: number;
    // the rooms or areas drawn:
    items: Set<number>;
    done: boolean;
    error: string;
}

// useUnderworldStream follows the drawing of the underworld map of the ROM `romKey` names ("" for none).
export function useUnderworldStream(romKey: string): MapStream {
    return useMapStream("underworld", "room", romKey);
}

// useOverworldStream follows the drawing of the overworld's areas of the ROM `romKey` names ("" for none).
export function useOverworldStream(romKey: string): MapStream {
    return useMapStream("overworld", "area", romKey);
}

function useMapStream(map: string, item: string, romKey: string): MapStream {
    const [state, setState] = useState<MapStream>({id: 0, items: new Set(), done: false, error: ""});
    useEffect(() => {
        setState({id: 0, items: new Set(), done: false, error: ""});
        if (!romKey) return;

        const es = new EventSource(`/rom/${map}/events?r=${romKey}`);
        // rooms arrive many at a time; render them in batches:
        let pending: number[] = [];
        let timer = 0;
        const flush = () => {
            timer = 0;
            const add = pending;
            pending = [];
            setState(s => {
                const items = new Set(s.items);
                add.forEach(r => items.add(r));
                return {...s, items};
            });
        };
        es.addEventListener("map", (e: MessageEvent) => {
            const id = parseInt(e.data, 10);
            setState(s => s.id === id ? s : {id, items: new Set(), done: false, error: ""});
        });
        es.addEventListener(item, (e: MessageEvent) => {
            pending.push(parseInt(e.data, 10));
            if (!timer) timer = window.setTimeout(flush, 100);
        });
        es.addEventListener("done", () => {
            es.close();
            flush();
            setState(s => ({...s, done: true}));
        });
        es.addEventListener("failed", (e: MessageEvent) => {
            es.close();
            flush();
            setState(s => ({...s, done: true, error: e.data || "the map could not be drawn from this ROM"}));
        });
        return () => {
            es.close();
            if (timer) window.clearTimeout(timer);
        };
    }, [romKey, map, item]);
    return state;
}

export function roomImageURL(id: number, room: number, full: boolean): string {
    return `/rom/underworld/room.png?id=${id}&room=${room}` + (full ? "&full=1" : "");
}

function placeImageURL(id: number, p: Place): string {
    return p.kind === "room" ? roomImageURL(id, p.id, true) : `/rom/overworld/area.png?id=${id}&area=${p.id}`;
}

function placeWalkURL(id: number, p: Place): string {
    return p.kind === "room" ? `/rom/underworld/walk.json?id=${id}&room=${p.id}`
        : `/rom/overworld/walk.json?id=${id}&area=${p.id}`;
}

// PlaceWalk is where Link can be in a room or an area.
interface PlaceWalk {
    // the dungeon ($040C) the room is in, $FF for caves and houses and the overworld:
    palace: number;
    // the layers Link can be on: bit 0 BG1, bit 1 BG2 (an area has BG1 only):
    layers: number;
    // the number of cells of a side of a layer:
    side: number;
    // the cells of BG1 then BG2:
    cells: Uint8Array;
}

// the walks and full-size images loaded, by map id and place:
const walks = new Map<string, Promise<PlaceWalk | null>>();
const images = new Map<string, HTMLImageElement>();

function placeKey(id: number, p: Place): string {
    return id + ":" + p.kind + ":" + p.id;
}

function loadWalk(id: number, place: Place): Promise<PlaceWalk | null> {
    const key = placeKey(id, place);
    let p = walks.get(key);
    if (!p) {
        p = fetch(placeWalkURL(id, place))
            .then(r => r.ok ? r.json() : null)
            .then(j => {
                if (!j) return null;
                const bin = atob(j.cells);
                const cells = new Uint8Array(bin.length);
                for (let i = 0; i < bin.length; i++) cells[i] = bin.charCodeAt(i);
                if (place.kind === "area") return {palace: 0xFF, layers: 1, side: j.size / 8, cells};
                return {palace: j.palace ?? 0xFF, layers: j.layers, side: 64, cells};
            })
            .catch(() => null);
        walks.set(key, p);
        p.then(w => {
            if (!w) walks.delete(key);
        });
    }
    return p;
}

function loadImage(id: number, place: Place, onLoad: () => void): HTMLImageElement {
    const key = placeKey(id, place);
    let img = images.get(key);
    if (!img) {
        img = new Image();
        img.src = placeImageURL(id, place);
        images.set(key, img);
    }
    if (!img.complete) img.addEventListener("load", onLoad, {once: true});
    return img;
}

// how much farther (in pixels) a reachable point may be than the nearest point where Link can stand, to be preferred:
const preferReachable = 24;

// snap finds where Link is placed for the cursor at px, py of the room or area: the point whose feet's center (x + 8,
// y + 16) is nearest among the points where he can stand, unless a reachable one is barely farther. A point the
// flood fill didn't reach may still be reachable in ways it doesn't know.
export function snap(place: Place, walk: PlaceWalk, px: number, py: number): WarpTarget | null {
    let reach: WarpTarget | null = null, stand: WarpTarget | null = null;
    let reachD = Infinity, standD = Infinity;
    const n = walk.side;
    // a large area has 16384 cells: only those within half a screen of the cursor:
    const reach8 = n > 64 ? 16 : n;
    const r0 = Math.max(0, Math.floor(py / 8) - reach8), r1 = Math.min(n, Math.floor(py / 8) + reach8 + 2);
    const c0 = Math.max(0, Math.floor(px / 8) - reach8), c1 = Math.min(n, Math.floor(px / 8) + reach8 + 2);
    for (let l = 0; l < 2; l++) {
        if ((walk.layers & (1 << l)) === 0) continue;
        for (let row = r0; row < r1; row++) for (let col = c0; col < c1; col++) {
            const f = walk.cells[l * n * n + row * n + col];
            if ((f & WalkStandable) === 0) continue;
            const x = col * 8;
            const y = row * 8 - 8;
            const dx = x + 8 - px;
            const dy = y + 16 - py;
            const d = Math.sqrt(dx * dx + dy * dy);
            const reachable = (f & WalkReachable) !== 0;
            const t: WarpTarget = place.kind === "area"
                ? {kind: "area", area: place.id, x, y, reachable}
                : {
                    kind: "room", room: place.id, x, y, layer: l, reachable, lower: (f & WalkLower) !== 0,
                    palace: walk.palace,
                };
            if (d < standD) {
                standD = d;
                stand = t;
            }
            if (t.reachable && d < reachD) {
                reachD = d;
                reach = t;
            }
        }
    }
    if (reach && reachD <= standD + preferReachable) return reach;
    return stand;
}

export function hex(value: number, digits: number): string {
    return value.toString(16).toUpperCase().padStart(digits, "0");
}

// placeText is the dungeon, number and name of a room, e.g. "Eastern Palace $0C9 Entrance":
export function placeText(room: number, palace: number | undefined): string {
    const {dungeon, name} = placeNames(room, palace);
    return (dungeon ? dungeon + " " : "") + "$" + hex(room, 3) + (name ? " " + name : "");
}

// areaText is the world and number of an area, e.g. "Light World $1B":
export function areaText(area: number): string {
    return (area & 0x40 ? "Dark World" : "Light World") + " $" + hex(area, 2);
}

export function targetText(t: WarpTarget): string {
    if (t.kind === "area") {
        return `${areaText(t.area)} at ($${hex(t.x, 3)}, $${hex(t.y, 3)})` + (t.reachable ? "" : ", maybe cut off");
    }
    return `${placeText(t.room, t.palace)} at ($${hex(t.x, 3)}, $${hex(t.y, 3)}) on ${layerText(t)}` +
        (t.reachable ? "" : ", maybe cut off");
}

// layerText is the layer whose collision Link has, and the level he is on where it isn't the layer's:
function layerText(t: WarpTarget): string {
    if (t.kind === "area") return "";
    return (t.layer ? "BG2" : "BG1") + (t.lower !== !!t.layer ? (t.lower ? " lower" : " upper") : "");
}

// the zoomed-in view: lensSize CSS pixels showing lensSize / lensZoom pixels of the room:
const lensSize = 224;
const lensZoom = 2;

export interface LensProps {
    mapId: number;
    place: Place;
    // the cursor in the room's or area's pixels:
    px: number;
    py: number;
    // where the lens goes in its container, in CSS pixels, and whether it goes left or above of that point:
    left: number;
    top: number;
    flipX: boolean;
    flipY: boolean;
    // called with where Link would be placed (null if nowhere or not known yet):
    onSnap: (t: WarpTarget | null) => void;
}

export function WarpLens(p: LensProps) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const [loaded, setLoaded] = useState(0);

    const {kind, id} = p.place;
    const [walk, setWalk] = useState<PlaceWalk | null | undefined>(undefined);
    useEffect(() => {
        let live = true;
        setWalk(undefined);
        loadWalk(p.mapId, p.place).then(w => {
            if (live) setWalk(w);
        });
        return () => {
            live = false;
        };
    }, [p.mapId, kind, id]);

    const target = walk ? snap(p.place, walk, p.px, p.py) : null;
    const targetLayer = target?.kind === "room" ? target.layer : 0;
    useEffect(() => p.onSnap(target), [target?.x, target?.y, targetLayer, kind, id, walk]);

    useEffect(() => {
        const c = canvas.current;
        if (!c) return;
        const ctx = c.getContext("2d");
        if (!ctx) return;
        const img = loadImage(p.mapId, p.place, () => setLoaded(n => n + 1));
        const span = lensSize / lensZoom;
        const x0 = Math.round(p.px - span / 2);
        const y0 = Math.round(p.py - span / 2);

        ctx.imageSmoothingEnabled = false;
        ctx.fillStyle = "black";
        ctx.fillRect(0, 0, lensSize, lensSize);
        if (img.complete && img.naturalWidth) {
            ctx.drawImage(img, x0, y0, span, span, 0, 0, lensSize, lensSize);
        }

        // what Link can't walk on is darkened, pits and deep water tinted red and what does something when walked on
        // (staircases, conveyors, spikes, ...) yellow, on the layer of the target:
        if (walk) {
            const n = walk.side;
            const l = target ? targetLayer : (walk.layers & 1 ? 0 : 1);
            for (let row = Math.max(0, Math.floor(y0 / 8)); row < Math.min(n, Math.ceil((y0 + span) / 8)); row++) {
                for (let col = Math.max(0, Math.floor(x0 / 8)); col < Math.min(n, Math.ceil((x0 + span) / 8)); col++) {
                    const f = walk.cells[l * n * n + row * n + col];
                    if ((f & WalkPassable) === 0) {
                        ctx.fillStyle = (f & WalkHazard) ? "rgba(255, 0, 0, 0.4)" : "rgba(0, 0, 0, 0.55)";
                    } else if (f & WalkHazard) {
                        ctx.fillStyle = "rgba(255, 200, 0, 0.3)";
                    } else {
                        continue;
                    }
                    ctx.fillRect((col * 8 - x0) * lensZoom, (row * 8 - y0) * lensZoom, 8 * lensZoom, 8 * lensZoom);
                }
            }
        }

        // where Link would be: his 16x24 body, his feet filled:
        if (target) {
            const lx = (target.x - x0) * lensZoom;
            const ly = (target.y - y0) * lensZoom;
            ctx.fillStyle = target.reachable ? "rgba(0, 255, 0, 0.35)" : "rgba(255, 200, 0, 0.45)";
            ctx.fillRect(lx, ly + 8 * lensZoom, 16 * lensZoom, 16 * lensZoom);
            ctx.strokeStyle = target.reachable ? "rgb(0, 255, 0)" : "rgb(255, 200, 0)";
            ctx.lineWidth = 1;
            ctx.strokeRect(lx + 0.5, ly + 0.5, 16 * lensZoom - 1, 24 * lensZoom - 1);
        }

        // the cursor:
        const cx = lensSize / 2 + 0.5;
        ctx.strokeStyle = "white";
        ctx.beginPath();
        ctx.moveTo(cx - 8, cx);
        ctx.lineTo(cx + 8, cx);
        ctx.moveTo(cx, cx - 8);
        ctx.lineTo(cx, cx + 8);
        ctx.stroke();
    }, [p.mapId, kind, id, p.px, p.py, walk, loaded, target?.x, target?.y, targetLayer]);

    const caption = walk === undefined ? "loading…"
        : walk === null ? "can't warp here"
            : target ? `→ ($${hex(target.x, 3)}, $${hex(target.y, 3)})` + (target.kind === "room" ? " " + layerText(target) : "") +
                (target.reachable ? "" : " (maybe cut off)")
                : "no place to stand";
    const style = `left: ${p.left}px; top: ${p.top}px; transform: translate(${p.flipX ? "calc(-100% - 16px)" : "16px"}, ` +
        `${p.flipY ? "calc(-100% - 16px)" : "16px"})`;
    return <div class="warp-lens" style={style}>
        <div class="warp-lens-caption warp-lens-place">
            {kind === "room" ? placeText(id, walk ? walk.palace : undefined) : areaText(id)}
        </div>
        <canvas ref={canvas} width={lensSize} height={lensSize}/>
        <div class="warp-lens-caption mono">
            (${hex(Math.floor(p.px), 3)}, ${hex(Math.floor(p.py), 3)}) {caption}
        </div>
    </div>;
}
