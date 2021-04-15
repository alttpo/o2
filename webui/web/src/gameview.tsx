import {GameViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";

type GameProps = {
    ch: CommandHandler;
    game: GameViewModel;
};

function GameView({ch, game}: GameProps) {
    const [team, setTeam] = useState(0);
    const [playerName, setPlayerName] = useState('');

    useEffect(() => {
        setTeam(game.team);
        setPlayerName(game.playerName);
    }, [game]);

    function onInput<T>(
        setter: (arg0: T) => void,
        fieldName: any,
        coerceValue: (strValue: string) => T,
        e: Event
    ) {
        const strValue: string = (e.target as HTMLInputElement).value;
        const coerced: T = coerceValue(strValue);
        setter(coerced);
        ch.command(
            "game",
            "setField",
            {
                [fieldName]: coerced
            }
        );
    }

    return <div class="card three-grid">
        <label class="grid-col1" for="playerName">Player Name:</label>
        <input type="text" value={playerName} id="playerName"
               onInput={onInput.bind(this, setPlayerName, "playerName", (v: string) => v)}/>
        <label class="grid-col1" for="team">Team Number:</label>
        <input type="number" min={0} max={255} value={team} id="team"
               onInput={onInput.bind(this, setTeam, "team", (v: string) => parseInt(v, 10))}/>
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<GameView ch={ch} game={vm.game}/>);
