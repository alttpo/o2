import {GameALTTPViewModel, GameViewProps} from "../viewmodel";
import {useEffect, useRef, useState} from "preact/hooks";
import {Fragment} from "preact";
import {setField} from "../util";

export function GameViewALTTP({ch, vm}: GameViewProps) {
    const game = vm.game as GameALTTPViewModel;
    const [syncItems, setsyncItems] = useState(true);
    const [syncDungeonItems, setsyncDungeonItems] = useState(true);
    const [syncProgress, setsyncProgress] = useState(true);
    const [syncHearts, setsyncHearts] = useState(true);
    const [syncSmallKeys, setsyncSmallKeys] = useState(true);
    const [syncUnderworld, setsyncUnderworld] = useState(true);
    const [syncOverworld, setsyncOverworld] = useState(true);
    const [syncChests, setsyncChests] = useState(true);

    const [notifHistory, setNotifHistory] = useState([] as string[]);
    const historyTextarea = useRef(null);

    const [showASM, set_showASM] = useState(false);
    const [code, set_code] = useState('A903 8F59F37E');

    useEffect(() => {
        setsyncItems(game.syncItems);
        setsyncDungeonItems(game.syncDungeonItems);
        setsyncProgress(game.syncProgress);
        setsyncHearts(game.syncHearts);
        setsyncSmallKeys(game.syncSmallKeys);
        setsyncUnderworld(game.syncUnderworld);
        setsyncOverworld(game.syncOverworld);
        setsyncChests(game.syncChests);
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

    return <div style="display: grid; min-width: 20em; width: 100%">
        <h5 style="grid-column: 1">Game: {game.gameName}</h5>
        <div style="grid-column: 1">
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
        <div style="grid-column: 2; width: 100%; height: 100%; overflow: auto; display: grid; grid-template-columns: 1fr 7fr 1fr 1fr">
            {
                (vm["game/players"] || []).map((p: any) => (<Fragment key={p.index.toString()}>
                    <div class="mono" title="Player index">{("0" + p.index.toString(16)).substr(-2)}</div>
                    <div style="color: yellow; white-space: nowrap" title="Player name">{p.name}</div>
                    <div title="Team number">{p.team}</div>
                    <div title="Location">{p.location}</div>
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
