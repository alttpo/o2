// this view-model data comes from websocket JSON:
export class ViewModel {
    [k: string]: any;

    status: string;
    snes: SNESViewModel;
    rom: ROMViewModel;
    server: ServerViewModel;
    game: GameViewModel;
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

    devices: string[];
    selectedDevice: number;

    isConnected: boolean;
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
