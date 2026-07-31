import defineFunction from "../defineFunction";

defineFunction({
    type: "internal",
    names: ["\\relax"],
    numArgs: 0,
    allowedInText: true,
    allowedInArgument: true,

    handler({parser}) {
        return {
            type: "internal",
            mode: parser.mode,
        };
    },
});
