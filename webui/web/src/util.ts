
export function setField<T>(
    sendCommand: (cmd: string, args: any) => void,
    setter: (arg0: T) => void,
    fieldName: any,
    extractValue: (e: Event) => T,
    e: Event
) {
    const value: T = extractValue(e);
    setter(value);
    sendCommand(
        "setField",
        {
            [fieldName]: value
        }
    );
}
