import * as babel from "@babel/core"

const result = await babel.transformFileAsync("src/index.tsx", {
  plugins: [
    ["@babel/plugin-transform-react-jsx", {
      runtime: "classic",
      pragma: "h",
      pragmaFrag: "Fragment",
    }],
  ],
  presets: [
    ["@babel/preset-typescript", { isTSX: true, allExtensions: true }],
  ],
})

console.log(result.code)