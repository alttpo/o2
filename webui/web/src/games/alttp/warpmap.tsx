// The underworld map's rooms as the backend draws them, and picking a point of a room to warp Link to in debug mode.
//
// The backend draws the rooms of the loaded ROM one by one (see underworldmap/stream.go) and tells which ones are
// ready over server-sent events (/rom/underworld/events); the map shows each room as soon as it is. In debug mode,
// hovering a room shows a zoomed-in view of it under the cursor with where Link would be placed: the nearest point
// where he can stand that is reachable from the room's ways in (or any point where he can stand, if the room has
// none), from the room's walk cells (/rom/underworld/walk.json; see underworldmap/walk.go). Clicking pins that point
// as the target of the Warp section's "Warp to map point", and double-clicking warps Link there right away.
import {useEffect, useRef, useState} from "preact/hooks";
import {placeNames} from "./roomnames";

// the walk cell flags (underworldmap.Walk*):
const WalkStandable = 1 << 0;
const WalkReachable = 1 << 1;
const WalkPassable = 1 << 2;
const WalkHazard = 1 << 3;
const WalkLower = 1 << 4;

export const roomPixels = 512;

// WarpTarget is a point of an underworld room where Link can be placed: his coordinates in the room's pixels, as
// the game keeps them ($22, $20), and the layer ($EE).
export interface WarpTarget {
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

// warpOp is the debug op that warps Link to the target.
export function warpOp(t: WarpTarget) {
    return {t: "warp", w: "room", r: t.room, x: t.x, y: t.y, l: t.layer} as const;
}

export interface UnderworldStream {
    // the map's id, for the URLs of its rooms; 0 until the backend has said:
    id: number;
    rooms: Set<number>;
    done: boolean;
    error: string;
}

// useUnderworldStream follows the drawing of the underworld map of the ROM `romKey` names ("" for none).
export function useUnderworldStream(romKey: string): UnderworldStream {
    const [state, setState] = useState<UnderworldStream>({id: 0, rooms: new Set(), done: false, error: ""});
    useEffect(() => {
        setState({id: 0, rooms: new Set(), done: false, error: ""});
        if (!romKey) return;

        const es = new EventSource(`/rom/underworld/events?r=${romKey}`);
        // rooms arrive many at a time; render them in batches:
        let pending: number[] = [];
        let timer = 0;
        const flush = () => {
            timer = 0;
            const add = pending;
            pending = [];
            setState(s => {
                const rooms = new Set(s.rooms);
                add.forEach(r => rooms.add(r));
                return {...s, rooms};
            });
        };
        es.addEventListener("map", (e: MessageEvent) => {
            const id = parseInt(e.data, 10);
            setState(s => s.id === id ? s : {id, rooms: new Set(), done: false, error: ""});
        });
        es.addEventListener("room", (e: MessageEvent) => {
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
    }, [romKey]);
    return state;
}

export function roomImageURL(id: number, room: number, full: boolean): string {
    return `/rom/underworld/room.png?id=${id}&room=${room}` + (full ? "&full=1" : "");
}

interface RoomWalk {
    // the dungeon ($040C) the room is in, $FF for caves and houses:
    palace: number;
    // the layers Link can be on: bit 0 BG1, bit 1 BG2:
    layers: number;
    // the cells of BG1 then BG2:
    cells: Uint8Array;
}

// the walks and full-size images loaded, by map id and room:
const walks = new Map<string, Promise<RoomWalk | null>>();
const images = new Map<string, HTMLImageElement>();

function loadWalk(id: number, room: number): Promise<RoomWalk | null> {
    const key = id + ":" + room;
    let p = walks.get(key);
    if (!p) {
        p = fetch(`/rom/underworld/walk.json?id=${id}&room=${room}`)
            .then(r => r.ok ? r.json() : null)
            .then(j => {
                if (!j) return null;
                const bin = atob(j.cells);
                const cells = new Uint8Array(bin.length);
                for (let i = 0; i < bin.length; i++) cells[i] = bin.charCodeAt(i);
                return {palace: j.palace ?? 0xFF, layers: j.layers, cells};
            })
            .catch(() => null);
        walks.set(key, p);
        p.then(w => {
            if (!w) walks.delete(key);
        });
    }
    return p;
}

function loadImage(id: number, room: number, onLoad: () => void): HTMLImageElement {
    const key = id + ":" + room;
    let img = images.get(key);
    if (!img) {
        img = new Image();
        img.src = roomImageURL(id, room, true);
        images.set(key, img);
    }
    if (!img.complete) img.addEventListener("load", onLoad, {once: true});
    return img;
}

// how much farther (in pixels) a reachable point may be than the nearest point where Link can stand, to be preferred:
const preferReachable = 24;

// snap finds where Link is placed for the cursor at px, py of the room: the point whose feet's center (x + 8,
// y + 16) is nearest among the points where he can stand, unless a reachable one is barely farther. A point the
// flood fill didn't reach may still be reachable in ways it doesn't know.
export function snap(room: number, walk: RoomWalk, px: number, py: number): WarpTarget | null {
    let reach: WarpTarget | null = null, stand: WarpTarget | null = null;
    let reachD = Infinity, standD = Infinity;
    for (let l = 0; l < 2; l++) {
        if ((walk.layers & (1 << l)) === 0) continue;
        for (let i = 0; i < 4096; i++) {
            const f = walk.cells[l * 4096 + i];
            if ((f & WalkStandable) === 0) continue;
            const x = (i & 63) * 8;
            const y = (i >> 6) * 8 - 8;
            const dx = x + 8 - px;
            const dy = y + 16 - py;
            const d = Math.sqrt(dx * dx + dy * dy);
            const t = {
                room, x, y, layer: l, reachable: (f & WalkReachable) !== 0, lower: (f & WalkLower) !== 0,
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

export function targetText(t: WarpTarget): string {
    return `${placeText(t.room, t.palace)} at ($${hex(t.x, 3)}, $${hex(t.y, 3)}) on ${layerText(t)}` +
        (t.reachable ? "" : ", maybe cut off");
}

// layerText is the layer whose collision Link has, and the level he is on where it isn't the layer's:
function layerText(t: WarpTarget): string {
    return (t.layer ? "BG2" : "BG1") + (t.lower !== !!t.layer ? (t.lower ? " lower" : " upper") : "");
}

// the zoomed-in view: lensSize CSS pixels showing lensSize / lensZoom pixels of the room:
const lensSize = 224;
const lensZoom = 2;

export interface LensProps {
    mapId: number;
    room: number;
    // the cursor in the room's pixels:
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
    const [walk, setWalk] = useState<RoomWalk | null | undefined>(undefined);
    const [loaded, setLoaded] = useState(0);

    useEffect(() => {
        let live = true;
        setWalk(undefined);
        loadWalk(p.mapId, p.room).then(w => {
            if (live) setWalk(w);
        });
        return () => {
            live = false;
        };
    }, [p.mapId, p.room]);

    const target = walk ? snap(p.room, walk, p.px, p.py) : null;
    useEffect(() => p.onSnap(target), [target?.x, target?.y, target?.layer, p.room, walk]);

    useEffect(() => {
        const c = canvas.current;
        if (!c) return;
        const ctx = c.getContext("2d");
        if (!ctx) return;
        const img = loadImage(p.mapId, p.room, () => setLoaded(n => n + 1));
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
            const l = target?.layer ?? (walk.layers & 1 ? 0 : 1);
            for (let row = Math.max(0, Math.floor(y0 / 8)); row < Math.min(64, Math.ceil((y0 + span) / 8)); row++) {
                for (let col = Math.max(0, Math.floor(x0 / 8)); col < Math.min(64, Math.ceil((x0 + span) / 8)); col++) {
                    const f = walk.cells[l * 4096 + row * 64 + col];
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
    }, [p.mapId, p.room, p.px, p.py, walk, loaded, target?.x, target?.y, target?.layer]);

    const caption = walk === undefined ? "loading…"
        : walk === null ? "can't warp here"
            : target ? `→ ($${hex(target.x, 3)}, $${hex(target.y, 3)}) ${layerText(target)}` +
                (target.reachable ? "" : " (maybe cut off)")
                : "no place to stand";
    const style = `left: ${p.left}px; top: ${p.top}px; transform: translate(${p.flipX ? "calc(-100% - 16px)" : "16px"}, ` +
        `${p.flipY ? "calc(-100% - 16px)" : "16px"})`;
    return <div class="warp-lens" style={style}>
        <div class="warp-lens-caption warp-lens-place">{placeText(p.room, walk ? walk.palace : undefined)}</div>
        <canvas ref={canvas} width={lensSize} height={lensSize}/>
        <div class="warp-lens-caption mono">
            (${hex(Math.floor(p.px), 3)}, ${hex(Math.floor(p.py), 3)}) {caption}
        </div>
    </div>;
}
