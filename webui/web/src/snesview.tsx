import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {Component, Fragment} from 'preact';

type SNESDriverProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
    drv: DriverViewModel;
};
type SNESDriverState = {
    deviceIndex: number;
    selectedDevice: number;
};

class SNESDriverView extends Component<SNESDriverProps, SNESDriverState> {
    constructor() {
        super();
        this.state = {deviceIndex: 0, selectedDevice: 0};
    }

    static getDerivedStateFromProps(props: SNESDriverProps, state: SNESDriverState): SNESDriverState {
        if (props.drv.selectedDevice != state.selectedDevice) {
            return {deviceIndex: props.drv.selectedDevice, selectedDevice: props.drv.selectedDevice};
        } else {
            return {deviceIndex: state.deviceIndex, selectedDevice: props.drv.selectedDevice};
        }
    }

    render({ch, snes, drv}: SNESDriverProps, state: SNESDriverState) {
        const cmdConnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'connect', {driver: drv.name, device: state.deviceIndex});
        }
        const cmdDisconnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'disconnect', {driver: drv.name});
        }

        const connectButton = (drv: DriverViewModel) => {
            if (drv.isConnected) {
                return <button type="button"
                               title={drv.displayDescription}
                               onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
            } else {
                return <button type="button"
                               title={drv.displayDescription}
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == 0)}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;

        return <Fragment key={name}>
            <label for={`device-${name}`}
                   style="white-space: nowrap"
                   title={drv.displayDescription}>{drv.displayName} driver:</label>
            <select
                disabled={snes.isConnected && !drv.isConnected}
                id={`device-${name}`}
                title={drv.displayDescription}
                onChange={(e) => this.setState({deviceIndex: (e.currentTarget.selectedIndex)})}>
                <option selected={0 == drv.selectedDevice}>(Select {drv.displayName} Device)</option>
                {(drv.devices || []).map((dev, i) =>
                    <option selected={(i + 1) == drv.selectedDevice}>{dev}</option>
                )}
            </select>
            {connectButton(drv)}
        </Fragment>;
    }
}

export default ({ch, vm}: TopLevelProps) => {
    return (<div style="display: table; min-width: 36em; width: 100%">
        <div style="display: table-row; height: 100%;">
            <div style="display: table-cell">
                <div style="display: grid; grid-template-columns: 1fr 3fr 1fr;">
                    <h5 style="grid-column: 1 / span 3">
                <span data-rh-at="left" data-rh="Select one of the below SNES drivers to connect to your SNES device.
Devices are auto-detected every 2 seconds for each driver."
                >Select a SNES device:&nbsp;1️⃣</span>
                    </h5>
                    {
                        (vm.snes?.drivers || []).map(drv => (
                            <SNESDriverView ch={ch} snes={vm.snes} drv={drv}/>
                        ))
                    }
                </div>
            </div>
        </div>
        {
            (vm.snes?.drivers?.some(value => value.name == "qusb2snes"))
                ? <div style="display: table-row; height: 100%">
                    <div style="display: table-cell; height: 5em">
                        <span style="position: absolute; bottom: 0">
<a href="https://github.com/Skarsnik/QUsb2snes/releases" target="_blank">QUsb2Snes</a>{' '}
is only required when connecting to an emulator. Recommended emulator is RetroArch 1.9.0 with bsnes-mercury core;{' '}
follow the setup instructions <a href="https://skarsnik.github.io/QUsb2snes/#retroarch" target="_blank">here</a>.{' '}
<strong>IMPORTANT:</strong> RA 1.9.1 does NOT work. Use RA 1.9.0 and earlier versions.<br/>
For SD2SNES / FX Pak Pro, use the <a href="https://github.com/alttpo/o2/tree/main/content/fxpakpro/firmware" target="_blank">
recommended firmware</a>.
                        </span>
                    </div>
                </div>
                : <Fragment/>
        }
    </div>);
}
;
