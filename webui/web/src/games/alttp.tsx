import {GameALTTPViewModel, GameViewProps} from "../viewmodel";
import {useEffect, useState} from "preact/hooks";
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

    const sendGameCommand = ch.command.bind(ch, "game");

    const getTargetChecked = (e: Event) => (e.target as HTMLInputElement).checked;

    return <div class="grid">
        <h5 class="grid-ca">Game: {game.gameName}</h5>

        <label class="grid-c1" for="syncItems">
            <input type="checkbox"
                   id="syncItems"
                   checked={syncItems}
                   onChange={setField.bind(this, sendGameCommand, setsyncItems, "syncItems", getTargetChecked)}
            />Sync Items
        </label>

        <div class="grid-c2">
            <label for="showASM">
                <input type="checkbox"
                       id="showASM"
                       checked={showASM}
                       onChange={e => set_showASM((e.target as HTMLInputElement).checked)}
                />Custom ASM
            </label>
        </div>

        <label class="grid-c1" for="syncDungeonItems"
               title="Big Keys, Compasses, Maps">
            <input type="checkbox"
                   id="syncDungeonItems"
                   checked={syncDungeonItems}
                   onChange={setField.bind(this, sendGameCommand, setsyncDungeonItems, "syncDungeonItems", getTargetChecked)}
            />Sync Dungeon Items
        </label>

        <textarea class="grid-c2-1" id="asm" cols={40} rows={2} value={code}
                  style="height: 2.5em"
                  hidden={!showASM}
                  onChange={e => set_code((e.target as HTMLTextAreaElement).value)}/>
        <button class="grid-c2-2"
                hidden={!showASM}
                disabled={!vm.snes.isConnected}
                onClick={e => sendGameCommand('asm', {code: code})}
        >Execute
        </button>

        <label class="grid-c1" for="syncProgress">
            <input type="checkbox"
                   id="syncProgress"
                   checked={syncProgress}
                   onChange={setField.bind(this, sendGameCommand, setsyncProgress, "syncProgress", getTargetChecked)}
            />Sync Progress
        </label>

        <label class="grid-c1" for="syncHearts">
            <input type="checkbox"
                   id="syncHearts"
                   checked={syncHearts}
                   onChange={setField.bind(this, sendGameCommand, setsyncHearts, "syncHearts", getTargetChecked)}
            />Sync Hearts
        </label>

        <label class="grid-c1" for="syncSmallKeys">
            <input type="checkbox"
                   id="syncSmallKeys"
                   checked={syncSmallKeys}
                   onChange={setField.bind(this, sendGameCommand, setsyncSmallKeys, "syncSmallKeys", getTargetChecked)}
            />Sync Small Keys
        </label>

        <label class="grid-c1" for="syncUnderworld">
            <input type="checkbox"
                   id="syncUnderworld"
                   checked={syncUnderworld}
                   onChange={setField.bind(this, sendGameCommand, setsyncUnderworld, "syncUnderworld", getTargetChecked)}
            />Sync Underworld
        </label>

        <label class="grid-c1" for="syncOverworld">
            <input type="checkbox"
                   id="syncOverworld"
                   checked={syncOverworld}
                   onChange={setField.bind(this, sendGameCommand, setsyncOverworld, "syncOverworld", getTargetChecked)}
            />Sync Overworld
        </label>

        <label class="grid-c1" for="syncChests">
            <input type="checkbox"
                   id="syncChests"
                   checked={syncChests}
                   onChange={setField.bind(this, sendGameCommand, setsyncChests, "syncChests", getTargetChecked)}
            />Sync Chests
        </label>
    </div>;
}
