// this view-model data comes from websocket JSON:
import {CommandHandler} from "./index";
import {JSX} from "preact";

export interface ViewModel {
    [k: string]: any;

    status?: string;
    snes?: SNESViewModel;
    rom?: ROMViewModel;
    server?: ServerViewModel;
    game?: GameViewModel;
}

export interface SNESViewModel {
    drivers: DriverViewModel[];
    isConnected: boolean;
}

export interface DriverViewModel {
    name: string;

    displayName: string;
    displayDescription: string;
    displayOrder: number;

    devices: DeviceViewModel[];
    selectedDevice: string;

    isConnected: boolean;
}

export interface DeviceViewModel {
    id: string;
    displayName: string;

    [k: string]: any;
}

export interface ROMViewModel {
    isLoaded: boolean;

    name: string;
    title: string;
    region: string;
    version: string;
}

export interface ServerViewModel {
    isConnected: boolean;

    hostName: string;
    groupName: string;
    playerName: string;
    team: number;
}

export interface GameViewModel {
    isCreated: boolean;
    gameName: string;
}

export interface GameALTTPViewModel extends GameViewModel {
    syncItems: boolean;
    syncDungeonItems: boolean;
    syncProgress: boolean;
    syncHearts: boolean;
    syncSmallKeys: boolean;
    syncUnderworld: boolean;
    syncOverworld: boolean;
    syncChests: boolean;
}

export type GameViewProps = {
    ch: CommandHandler;
    vm: ViewModel;
};

export type GameViewComponent = ({ch, vm}: GameViewProps) => JSX.Element;
