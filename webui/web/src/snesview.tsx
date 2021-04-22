import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {Component, Fragment} from 'preact';

type SNESDriverProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
    drv: DriverViewModel;
};
type SNESDriverState = {
    deviceIndex: string;
    selectedDevice: string;
};

class SNESDriverView extends Component<SNESDriverProps, SNESDriverState> {
    constructor() {
        super();
        this.state = {deviceIndex: "", selectedDevice: ""};
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
            ch.command('snes', 'connect', {
                driver: drv.name,
                device: drv.devices.find(dv => dv.id == state.deviceIndex)
            });
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
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == "")}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;

        return <Fragment key={name}>
            <label for={`device-${name}`}
                   style="white-space: nowrap; padding-top: 0.35em"
                   title={drv.displayDescription}
            >{drv.displayName} driver:</label>
            <span style="color: green; font-size: 1.4em; padding-top: 0.15em; margin: 0 2px 0 auto;"
                  title={'' + (drv.devices?.length || 0) + ' device(s) found'}
            >({drv.devices?.length || 0})</span>
            <select
                disabled={snes.isConnected && !drv.isConnected}
                id={`device-${name}`}
                title={drv.displayDescription}
                onChange={(e) => this.setState({deviceIndex: (e.currentTarget.value)})}>
                <option selected={"" == drv.selectedDevice}>(Select {drv.displayName} Device)</option>
                {(drv.devices || []).map(dev =>
                    <option selected={dev.id == drv.selectedDevice}
                            value={dev.id}
                    >{dev.displayName}</option>
                )}
            </select>
            {connectButton(drv)}
        </Fragment>;
    }
}

export default ({ch, vm}: TopLevelProps) => {
    return (<div style="display: table; min-width: 36em; width: 100%; height: 100%">
        <div style="display: table-row; height: 100%">
            <div style="display: table-cell">
                <div style="display: grid; grid-template-columns: 4fr 1fr 10fr 4fr;">
                    <h5 style="grid-column: 1 / span 4">
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
            ((vm.snes?.drivers?.some(value => value.name == "fxpakpro"))
                ? <div style="display: table-row; height: 100%">
                    <div style="display: table-cell">
                        <div style="margin-top: 4px">
For SD2SNES / FX Pak Pro, use the <a href="https://github.com/alttpo/o2/tree/main/content/fxpakpro/firmware" target="_blank">
recommended firmware</a>.
                        </div>
                    </div>
                </div>
                : <Fragment/>)
        }
        {
            (vm.snes?.drivers?.some(value => value.name == "qusb2snes"))
                ? <div style="display: table-row; height: 100%">
                    <div style="display: table-cell">
                        <div style="margin-top: 4px">
<a href="https://github.com/Skarsnik/QUsb2snes/releases" target="_blank">QUsb2Snes</a>{' '}
is only required when connecting to an emulator. Recommended emulator is RetroArch 1.9.0 with bsnes-mercury core;{' '}
follow the setup instructions <a href="https://skarsnik.github.io/QUsb2snes/#retroarch" target="_blank">here</a>.{' '}
<strong>IMPORTANT:</strong> RA 1.9.1 does NOT work. Use RA 1.9.0 and earlier versions.<br/>
                        </div>
                    </div>
                </div>
                : <Fragment/>
        }
    </div>);
};
