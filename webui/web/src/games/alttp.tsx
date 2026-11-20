import {GameALTTPViewModel, GameViewProps, TimestampedNotification} from "../viewmodel";
import {useEffect, useLayoutEffect, useRef, useState} from "preact/hooks";
import {Fragment} from "preact";
import {setField} from "../util";

// localStorage key of the height the user resized the notification history to:
const historyHeightKey = "o2.alttp.notificationHistoryHeight";

// timingTitle describes a player's run timing verification and flags, for a tooltip.
function timingTitle(p: any): string {
    const lines: string[] = [];
    if (p.verification === "verified") {
        lines.push("Run timing verified");
    } else if (p.verification === "unverified") {
        lines.push("Run timing unverified: " + p.unverified);
    } else {
        lines.push("No run timing from this player");
    }
    if (p.timingNotes) {
        lines.push("Flags: " + p.timingNotes);
    }
    return lines.join("\n");
}

export function GameViewALTTP({ch, vm}: GameViewProps) {
    const game = vm.game as GameALTTPViewModel;

    // the group reset asks first, so that a stray click doesn't reset everyone else's run:
    const [confirmReset, setConfirmReset] = useState(false);
    useEffect(() => {
        if (!confirmReset) { return; }
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") { setConfirmReset(false); }
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [confirmReset]);
    const resetForEveryone = () => {
        setConfirmReset(false);
        sendGameCommand("reset", {});
    };
    // without a server connection there is no group to reset; Reset only clears this client's own state:
    const online = !!vm.server?.isConnected;

    const [colorRed, setcolorRed] = useState(31);
    const [colorGreen, setcolorGreen] = useState(31);
    const [colorBlue, setcolorBlue] = useState(31);
    const [playerColor, setplayerColor] = useState(0x7fff);

    const [syncItems, setsyncItems] = useState(true);
    const [syncDungeonItems, setsyncDungeonItems] = useState(true);
    const [syncProgress, setsyncProgress] = useState(true);
    const [syncHearts, setsyncHearts] = useState(true);
    const [syncSmallKeys, setsyncSmallKeys] = useState(true);
    const [syncUnderworld, setsyncUnderworld] = useState(true);
    const [syncOverworld, setsyncOverworld] = useState(true);
    const [syncUnderworldLocal, setsyncUnderworldLocal] = useState(true);
    const [syncOverworldLocal, setsyncOverworldLocal] = useState(true);
    const [syncTorches, setsyncTorches] = useState(true);
    const [syncEnemiesKilled, setsyncEnemiesKilled] = useState(true);
    const [killDeadEnemies, setkillDeadEnemies] = useState(true);
    const [syncSfx, setsyncSfx] = useState(true);
    const [syncChests, setsyncChests] = useState(true);
    const [syncTunicColor, setsyncTunicColor] = useState(true);
    const [syncSprites, setsyncSprites] = useState(true);

    const [notifHistory, setNotifHistory] = useState([] as TimestampedNotification[]);
    const historyTextarea = useRef<HTMLDivElement>(null);
    // the height the user resized the notification history to, kept in this browser:
    const [historyHeight] = useState(() => {
        try {
            return parseInt(window.localStorage.getItem(historyHeightKey) || "", 10) || 0;
        } catch {
            return 0;
        }
    });

    const [showASM, set_showASM] = useState(false);
    const asmTextAreaRef = useRef(null);

    const [runTimer, setrunTimer] = useState("");

    useEffect(() => {
        setplayerColor(game.playerColor);
        const blu5 = (game.playerColor & 0x7E00) >> 10;
        const grn5 = (game.playerColor & 0x03E0) >> 5;
        const red5 = (game.playerColor & 0x001F);
        setcolorRed(red5);
        setcolorGreen(grn5);
        setcolorBlue(blu5);

        setsyncItems(game.syncItems);
        setsyncDungeonItems(game.syncDungeonItems);
        setsyncProgress(game.syncProgress);
        setsyncHearts(game.syncHearts);
        setsyncSmallKeys(game.syncSmallKeys);
        setsyncUnderworld(game.syncUnderworld);
        setsyncOverworld(game.syncOverworld);
        setsyncUnderworldLocal(game.syncUnderworldLocal);
        setsyncOverworldLocal(game.syncOverworldLocal);
        setsyncTorches(game.syncTorches);
        setsyncEnemiesKilled(game.syncEnemiesKilled);
        setkillDeadEnemies(game.killDeadEnemies);
        setsyncSfx(game.syncSfx);
        setsyncChests(game.syncChests);
        setsyncTunicColor(game.syncTunicColor);
        setsyncSprites(game.syncSprites);
    }, [game]);

    useEffect(() => {
        let history = vm["game/notification/history"] as TimestampedNotification[];
        if (!history) {
            history = [];
        }
        setNotifHistory(history);
    }, [vm["game/notification/history"]]);

    useLayoutEffect(() => {
        // scroll to bottom:
        if (historyTextarea.current) {
            historyTextarea.current.scrollTop = historyTextarea.current.scrollHeight;
        }
    }, [vm["game/notification/history"]]);

    useEffect(() => {
        const el = historyTextarea.current;
        if (!el || typeof ResizeObserver === "undefined") return;
        let last = el.offsetHeight;
        const ro = new ResizeObserver(() => {
            // every element is box-sizing: border-box, so offsetHeight is the inline height to restore:
            const height = el.offsetHeight;
            if (height === last || height === 0) return;
            last = height;
            try {
                window.localStorage.setItem(historyHeightKey, height.toString());
            } catch {
                // the height is only a convenience
            }
        });
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    useEffect(() => {
        setrunTimer(vm["game/run/timer"]);
    }, [vm["game/run/timer"]]);

    const sendGameCommand = ch.command.bind(ch, "game");

    const getTargetChecked = (e: Event) => (e.target as HTMLInputElement).checked;

    // BGR order from MSB to LSB, 0bbbbbgggggrrrrr
    const bgr16 = (r: number, g: number, b: number) => ((b & 31) << 10) | ((g & 31) << 5) | (r & 31);

    const bgr16torgb24 = (u16: number) => {
        const blu5 = (u16 & 0x7E00) >> 10;
        const grn5 = (u16 & 0x03E0) >> 5;
        const red5 = (u16 & 0x001F);

        const blu8 = (blu5 << 3) | (blu5 >> 2);
        const grn8 = (grn5 << 3) | (grn5 >> 2);
        const red8 = (red5 << 3) | (red5 >> 2);

        const rgb24 = (red8 << 16) | (grn8 << 8) | blu8;
        return rgb24;
    };

    const hexrgb24 = (rgb: number) => {
        const hex = "#" + ("000000" + rgb.toString(16)).slice(-6);
        return hex;
    };

    const setColorValue = (colorPart: string, setcolorPart: (c: number) => void, e: Event) => {
        const value = (e.target as HTMLInputElement).valueAsNumber;
        setcolorPart(value);

        let bgr;
        switch (colorPart) {
            case 'red':
                bgr = bgr16(value, colorGreen, colorBlue);
                break;
            case 'green':
                bgr = bgr16(colorRed, value, colorBlue);
                break;
            case 'blue':
                bgr = bgr16(colorRed, colorGreen, value);
                break;
        }

        setplayerColor(bgr);
        sendGameCommand("setField", {"playerColor": bgr});
    };

    const setColorHex24 = (e: Event) => {
        const x = (e.target as HTMLInputElement).value.trim().toLowerCase();
        if (x.length != 6) { return; }

        console.log(x);
        let r = (parseInt(x.slice(0, 2), 16) | 0) >> 3;
        let g = (parseInt(x.slice(2, 4), 16) | 0) >> 3;
        let b = (parseInt(x.slice(4, 6), 16) | 0) >> 3;
        console.log(r, g, b);
        setcolorRed(r);
        setcolorGreen(g);
        setcolorBlue(b);
        let bgr = bgr16(r, g, b);

        setplayerColor(bgr);
        sendGameCommand("setField", {"playerColor": bgr});
    };

    return <div style="display: grid; min-width: 30em; width: 100%; grid-column-gap: 1.0em; grid-row-gap: 0.25em; grid-template-columns: minmax(24em, 2fr) 3fr;">
        <div style="grid-column: 1 / auto; display: grid; grid-template-columns: 1fr max-content max-content; grid-column-gap: 0.5em; align-items: center;">
            <h5>Game: {game.gameName}</h5>
            <button type="button" class="game-action"
                    title="Fix small keys"
                    onClick={e => sendGameCommand("fixSmallKeys", {})}>Fix Small Keys</button>
            <button type="button" class="game-action"
                    title={online
                        ? "Forget all game state and reset the run timer for all players in the group; does not reset the console/emulator; asks first"
                        : "Offline: forget this client's own game state and run timer; does not reset the console/emulator; asks first"}
                    onClick={e => setConfirmReset(true)}>Reset</button>
        </div>
        {confirmReset && (
            <div class="confirm-backdrop" onClick={e => { if (e.target === e.currentTarget) { setConfirmReset(false); } }}>
                <div class="confirm-reset rounded darken padded" role="alertdialog" aria-modal="true"
                     aria-labelledby="confirm-reset-title" aria-describedby="confirm-reset-body">
                    <h3 id="confirm-reset-title">{online ? "Reset the run for everyone?" : "Reset your own client state?"}</h3>
                    {online ? (<div id="confirm-reset-body">
                        <p>
                            This resets the run timer for every player in the group, alttpo players included, and
                            clears all synchronized game state for everyone.
                        </p>
                        <p class="confirm-reset-howto">
                            To fully reset, every player first resets their console or emulator, and then one player
                            presses Reset. A player still in the old game would sync its state back to everyone after
                            the reset.
                        </p>
                    </div>) : (<div id="confirm-reset-body">
                        <p>
                            You are offline: not connected to the server. This only resets your own client state:
                            it clears the game state and players o2 remembers, and your run timer. No other player is
                            affected.
                        </p>
                    </div>)}
                    <div class="confirm-buttons">
                        <button type="button" autofocus={true} onClick={e => setConfirmReset(false)}>Cancel</button>
                        <button type="button" class="confirm-danger" onClick={e => resetForEveryone()}>{online ? "Reset for everyone" : "Reset my state"}</button>
                    </div>
                </div>
            </div>
        )}
        <div style="grid-column: 1">
            <h5>Sync:</h5>
            <div style="display: grid; grid-template-columns: 1fr 1fr 6fr;">
                <label for="syncTunicColor" style="grid-column: 1 / span 2">
                    <input type="checkbox"
                           id="syncTunicColor"
                           checked={syncTunicColor}
                           onChange={setField.bind(this, sendGameCommand, setsyncTunicColor, "syncTunicColor", getTargetChecked)}
                    />Tunic Color:
                </label>
                <div>
                    <input type="text" readonly={true} value={("0000" + playerColor.toString(16).toUpperCase()).slice(-4)} title="BGR555"/>
                    <input type="text" value={("000000" + bgr16torgb24(playerColor).toString(16).toUpperCase()).slice(-6)}
                           title="RGB888"
                           onInput={setColorHex24.bind(this)}
                    />
                </div>

                <div style={
                    "grid-column: 1; grid-row: 2 / span 3; border: 1px solid white; margin: 6px; background-color: " +
                    hexrgb24(bgr16torgb24(playerColor))
                }/>
                <label for="red">red:</label>
                <input id="red" class="no-padding-margin"
                       type="range" min={0} max={31} step={1}
                       value={colorRed} onInput={setColorValue.bind(this, 'red', setcolorRed)}/>
                <label for="green">green:</label>
                <input id="green" class="no-padding-margin"
                       type="range" min={0} max={31} step={1}
                       value={colorGreen} onInput={setColorValue.bind(this, 'green', setcolorGreen)}/>
                <label for="blue">blue:</label>
                <input id="blue" class="no-padding-margin"
                       type="range" min={0} max={31} step={1}
                       value={colorBlue} onInput={setColorValue.bind(this, 'blue', setcolorBlue)}/>
            </div>
            <div style="display: grid; grid-template-columns: 1fr 1fr;">
                <div><label for="syncItems">
                    <input type="checkbox"
                           id="syncItems"
                           checked={syncItems}
                           onChange={setField.bind(this, sendGameCommand, setsyncItems, "syncItems", getTargetChecked)}
                    />Items
                </label></div>

                <div><label for="syncDungeonItems"
                       title="Big Keys, Compasses, Maps">
                    <input type="checkbox"
                           id="syncDungeonItems"
                           checked={syncDungeonItems}
                           onChange={setField.bind(this, sendGameCommand, setsyncDungeonItems, "syncDungeonItems", getTargetChecked)}
                    />Dungeon Items
                </label></div>

                <div><label for="syncProgress">
                    <input type="checkbox"
                           id="syncProgress"
                           checked={syncProgress}
                           onChange={setField.bind(this, sendGameCommand, setsyncProgress, "syncProgress", getTargetChecked)}
                    />Progress
                </label></div>

                <div><label for="syncHearts">
                    <input type="checkbox"
                           id="syncHearts"
                           checked={syncHearts}
                           onChange={setField.bind(this, sendGameCommand, setsyncHearts, "syncHearts", getTargetChecked)}
                    />Hearts
                </label></div>

                <div><label for="syncSmallKeys">
                    <input type="checkbox"
                           id="syncSmallKeys"
                           checked={syncSmallKeys}
                           onChange={setField.bind(this, sendGameCommand, setsyncSmallKeys, "syncSmallKeys", getTargetChecked)}
                    />Small Keys
                </label></div>

                <div><label for="syncChests">
                    <input type="checkbox"
                           id="syncChests"
                           checked={syncChests}
                           onChange={setField.bind(this, sendGameCommand, setsyncChests, "syncChests", getTargetChecked)}
                    />Chests
                </label></div>

                <div><label for="syncOverworld"
                       title="Overworld area flags kept by every player: opened entrances, bombed walls, heart pieces and other events, once the area loads">
                    <input type="checkbox"
                           id="syncOverworld"
                           checked={syncOverworld}
                           onChange={setField.bind(this, sendGameCommand, setsyncOverworld, "syncOverworld", getTargetChecked)}
                    />Overworld (Global)
                </label></div>

                <div><label for="syncUnderworld"
                       title="Dungeon room flags kept by every player: chests, keys, doors and bosses, once the room loads">
                    <input type="checkbox"
                           id="syncUnderworld"
                           checked={syncUnderworld}
                           onChange={setField.bind(this, sendGameCommand, setsyncUnderworld, "syncUnderworld", getTargetChecked)}
                    />Underworld (Global)
                </label></div>

                <div><label for="syncOverworldLocal"
                       title="What players in the same area do while there: lifted bushes and rocks and the secrets under them, bombed walls, entrance animations">
                    <input type="checkbox"
                           id="syncOverworldLocal"
                           checked={syncOverworldLocal}
                           onChange={setField.bind(this, sendGameCommand, setsyncOverworldLocal, "syncOverworldLocal", getTargetChecked)}
                    />Overworld (Local)
                </label></div>

                <div><label for="syncUnderworldLocal"
                       title="What players in the same room do while there: pots and the items under them, blocks, pegs, crystal switches, star tiles, trap doors, switches, chests, flying floor tiles">
                    <input type="checkbox"
                           id="syncUnderworldLocal"
                           checked={syncUnderworldLocal}
                           onChange={setField.bind(this, sendGameCommand, setsyncUnderworldLocal, "syncUnderworldLocal", getTargetChecked)}
                    />Underworld (Local)
                </label></div>

                <div><label for="syncTorches"
                       title="Torches lit by players in the same room">
                    <input type="checkbox"
                           id="syncTorches"
                           checked={syncTorches}
                           onChange={setField.bind(this, sendGameCommand, setsyncTorches, "syncTorches", getTargetChecked)}
                    />Torches
                </label></div>

                <div><label for="syncSfx"
                       title="Sound effects of players in the same room or area">
                    <input type="checkbox"
                           id="syncSfx"
                           checked={syncSfx}
                           onChange={setField.bind(this, sendGameCommand, setsyncSfx, "syncSfx", getTargetChecked)}
                    />Sound Effects
                </label></div>

                <div><label for="syncSprites"
                       title="Show other players in the same room or area">
                    <input type="checkbox"
                           id="syncSprites"
                           checked={syncSprites}
                           onChange={setField.bind(this, sendGameCommand, setsyncSprites, "syncSprites", getTargetChecked)}
                    />Sprites
                </label></div>

                <div><label for="syncEnemiesKilled"
                       title="Enemies killed by players in the same room or area stay dead when the game would spawn them again here; enemies themselves are not synced">
                    <input type="checkbox"
                           id="syncEnemiesKilled"
                           checked={syncEnemiesKilled}
                           onChange={setField.bind(this, sendGameCommand, setsyncEnemiesKilled, "syncEnemiesKilled", getTargetChecked)}
                    />Enemy Dead List
                </label></div>

                <div><label for="killDeadEnemies"
                       title="Immediately kill the enemies here that players in the same room or area killed, when their dead list updates; needs Enemy Dead List">
                    <input type="checkbox"
                           id="killDeadEnemies"
                           checked={killDeadEnemies}
                           onChange={setField.bind(this, sendGameCommand, setkillDeadEnemies, "killDeadEnemies", getTargetChecked)}
                    />Kill Dead Enemies
                </label></div>
            </div>
            <div style="display: grid; grid-template-columns: 3fr 1fr; margin-top: 0.5em">
                <div style="grid-column: 1 / span 2">
                    <label for="showASM">
                        <input type="checkbox"
                               id="showASM"
                               checked={showASM}
                               onChange={e => set_showASM((e.target as HTMLInputElement).checked)}
                        />Custom ASM
                    </label>
                </div>

                <textarea ref={asmTextAreaRef} cols={40} rows={3}
                          style="height: 3.8em"
                          title="enter 65816 opcodes in HEX format to execute"
                          hidden={!showASM} />
                <button hidden={!showASM}
                        disabled={!vm.snes.isConnected}
                        onClick={e => sendGameCommand('asm', {code: asmTextAreaRef?.current?.value})}
                >Execute
                </button>
            </div>
        </div>
        <div style="grid-row: 1; grid-column: 2; display: flex">
            <div style="flex: 1">Run Timer:</div><span class="mono timing-mono" title="First player start to last player finish">{runTimer}</span>
        </div>
        <div class="players" style="grid-row: 2; grid-column: 2; align-self: start">
            <h5 style="grid-column: 1 / -1">Players</h5>
            {vm["game/run/warning"]?.warning
                ? <div class="timing-warning" style="grid-column: 1 / -1">&#x26A0; {vm["game/run/warning"].warning}</div>
                : null}
            <div class="players-header">##</div>
            <div class="players-header">team</div>
            <div class="players-header">name</div>
            <div class="players-header">location</div>
            <div class="players-header">start</div>
            <div class="players-header">finish</div>
            <div class="players-header" title="Lag frames since the start">lag</div>
            <div class="players-header" title="Run timing verification">ok</div>
            {
                (vm["game/players"] || []).map((p: any) => (<Fragment key={p.index.toString()}>
                    <div class="mono" title="Player index">{("00" + p.index.toString(16).toUpperCase()).slice(-3)}</div>
                    <div class="mono" title="Team number">{p.team}</div>
                    <div style={"color: " + hexrgb24(bgr16torgb24(p.color))}
                         title="Player name">{p.name}</div>
                    <div
                        style={"color: " + (((p.location & 0x10000) != 0) ? "green" : "cyan")}
                        title="Location">{
                        ((p.location & 0x10000) != 0) ? p.underworld : p.overworld
                    }</div>
                    <div class="mono" title="Start">{p.relStart}</div>
                    <div class="mono" title={p.penalty ? "Finish, and the time added for running fast" : "Finish"}>
                        {p.relFinish}{p.penalty ? <span class="timing-penalty"> {p.penalty}</span> : null}
                    </div>
                    <div class="mono" title="Lag frames since the start">{p.verification ? p.lagFrames : ""}</div>
                    <div class={"timing-" + (p.verification || "unknown")}
                         title={timingTitle(p)}>{
                        p.verification === "verified" ? (p.timingNotes ? "\u2713*" : "\u2713")
                            : p.verification === "unverified" ? "\u26A0" : ""
                    }</div>
                </Fragment>))
            }
        </div>
        <div style="grid-column: 1 / -1; margin-top: 0.5em">
            <div>updates:</div>
            <div ref={historyTextarea}
                 class="notification-history"
                 style={historyHeight ? `height: ${historyHeight}px` : ""}>
                {notifHistory.map((h, index) => (<Fragment key={index}>
                    <span><span style="font-family: monospace; font-size: 0.8rem">{h.t}</span>&nbsp;&ndash;&nbsp;{h.m}</span>
                </Fragment>))}
            </div>
        </div>
    </div>;
}
