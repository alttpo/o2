import {GameALTTPViewModel, GameViewProps} from "../viewmodel";
import {useEffect, useRef, useState} from "preact/hooks";
import {Fragment} from "preact";
import {setField} from "../util";

export function GameViewALTTP({ch, vm}: GameViewProps) {
    const game = vm.game as GameALTTPViewModel;

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
    const [syncChests, setsyncChests] = useState(true);
    const [syncTunicColor, setsyncTunicColor] = useState(true);

    const [notifHistory, setNotifHistory] = useState([] as string[]);
    const historyTextarea = useRef(null);

    const [showASM, set_showASM] = useState(false);
    const [code, set_code] = useState('A903 8F59F37E');

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
        setsyncChests(game.syncChests);
        setsyncTunicColor(game.syncTunicColor);
    }, [game]);

    useEffect(() => {
        let history = vm["game/notification/history"] as string[];
        if (!history) {
            history = [];
        }
        setNotifHistory(history);
    }, [vm["game/notification/history"]]);

    const mounted = useRef(false);
    useEffect(() => {
        if (!mounted.current) {
            // do componentDidMount logic
            mounted.current = true;
        } else {
            // do componentDidUpdate logic
            // scroll to bottom:
            if (historyTextarea.current) {
                historyTextarea.current.scrollTop = historyTextarea.current.scrollHeight;
            }
        }
    });

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
        const hex = "#" + ("000000" + rgb.toString(16)).substr(-6);
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

    return <div style="display: grid; min-width: 20em; width: 100%; grid-column-gap: 1.0em; grid-row-gap: 0.25em;">
        <h5 style="grid-column: 1">Game: {game.gameName}</h5>
        <div style="grid-column: 1">
            <div style="display: grid; grid-template-columns: 2fr 2fr 5fr;">
                <label for="syncTunicColor" style="grid-column: 1 / span 2">
                    <input type="checkbox"
                           id="syncTunicColor"
                           checked={syncTunicColor}
                           onChange={setField.bind(this, sendGameCommand, setsyncTunicColor, "syncTunicColor", getTargetChecked)}
                    />Sync Tunic Color:
                </label>
                <input type="text" readonly={true} value={("0000" + playerColor.toString(16)).substr(-4)}/>

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
                <label for="syncItems">
                    <input type="checkbox"
                           id="syncItems"
                           checked={syncItems}
                           onChange={setField.bind(this, sendGameCommand, setsyncItems, "syncItems", getTargetChecked)}
                    />Sync Items
                </label>

                <label for="syncDungeonItems"
                       title="Big Keys, Compasses, Maps">
                    <input type="checkbox"
                           id="syncDungeonItems"
                           checked={syncDungeonItems}
                           onChange={setField.bind(this, sendGameCommand, setsyncDungeonItems, "syncDungeonItems", getTargetChecked)}
                    />Sync Dungeon Items
                </label>

                <label for="syncProgress">
                    <input type="checkbox"
                           id="syncProgress"
                           checked={syncProgress}
                           onChange={setField.bind(this, sendGameCommand, setsyncProgress, "syncProgress", getTargetChecked)}
                    />Sync Progress
                </label>

                <label for="syncHearts">
                    <input type="checkbox"
                           id="syncHearts"
                           checked={syncHearts}
                           onChange={setField.bind(this, sendGameCommand, setsyncHearts, "syncHearts", getTargetChecked)}
                    />Sync Hearts
                </label>

                <label for="syncSmallKeys">
                    <input type="checkbox"
                           id="syncSmallKeys"
                           checked={syncSmallKeys}
                           onChange={setField.bind(this, sendGameCommand, setsyncSmallKeys, "syncSmallKeys", getTargetChecked)}
                    />Sync Small Keys
                </label>

                <label for="syncUnderworld">
                    <input type="checkbox"
                           id="syncUnderworld"
                           checked={syncUnderworld}
                           onChange={setField.bind(this, sendGameCommand, setsyncUnderworld, "syncUnderworld", getTargetChecked)}
                    />Sync Underworld
                </label>

                <label for="syncOverworld">
                    <input type="checkbox"
                           id="syncOverworld"
                           checked={syncOverworld}
                           onChange={setField.bind(this, sendGameCommand, setsyncOverworld, "syncOverworld", getTargetChecked)}
                    />Sync Overworld
                </label>

                <label for="syncChests">
                    <input type="checkbox"
                           id="syncChests"
                           checked={syncChests}
                           onChange={setField.bind(this, sendGameCommand, setsyncChests, "syncChests", getTargetChecked)}
                    />Sync Chests
                </label>
            </div>
        </div>
        <h5 style="grid-row: 1; grid-column: 2">Players</h5>
        <div
            style="grid-column: 2; width: 100%; height: 100%; overflow: auto; display: grid; grid-auto-rows: min-content; grid-template-columns: 1em 2em 6em 9em; grid-column-gap: 0.5em">
            <div style="font-weight: bold">##</div>
            <div style="font-weight: bold">team</div>
            <div style="font-weight: bold">name</div>
            <div style="font-weight: bold">location</div>
            {
                (vm["game/players"] || []).map((p: any) => (<Fragment key={p.index.toString()}>
                    <div class="mono" title="Player index">{("0" + p.index.toString(16)).substr(-2)}</div>
                    <div class="mono" title="Team number">{p.team}</div>
                    <div style="color: yellow; white-space: nowrap" title="Player name">{p.name}</div>
                    <div
                        style={"color: " + (((p.location & 0x10000) != 0) ? "green" : "cyan") + "; white-space: nowrap"}
                        title="Location">{
                        ((p.location & 0x10000) != 0) ? p.underworld : p.overworld
                    }</div>
                </Fragment>))
            }
        </div>
        <div style="grid-column: 1 / span 2">
            <div style="display: grid; grid-template-columns: 3fr 1fr;">
                <div style="grid-column: 1 / span 2">
                    <label for="showASM">
                        <input type="checkbox"
                               id="showASM"
                               checked={showASM}
                               onChange={e => set_showASM((e.target as HTMLInputElement).checked)}
                        />Custom ASM
                    </label>
                </div>

                <textarea id="asm" cols={40} rows={3} value={code}
                          style="height: 3.8em"
                          hidden={!showASM}
                          onChange={e => set_code((e.target as HTMLTextAreaElement).value)}/>
                <button hidden={!showASM}
                        disabled={!vm.snes.isConnected}
                        onClick={e => sendGameCommand('asm', {code: code})}
                >Execute
                </button>

                <div style="grid-column: 1 / span 2; margin-top: 0.5em">
                    updates:
                </div>
                <div style="grid-column: 1 / span 2; margin-top: 0.5em">
                    <textarea ref={historyTextarea}
                              value={notifHistory.join("\n")}
                              style="width: 100%; height: 5.8em; border: 1px solid red; background: #010; color: yellow; font-family: Rokkitt; font-size: 1.0em"
                              rows={5}
                              readonly={true}/>
                </div>
            </div>
        </div>
    </div>;
}
