// See https://encoding.spec.whatwg.org/#names-and-labels

/* eslint-disable @exodus/export-default/named */
// prettier-ignore
const labels = {
  'utf-8': ['unicode-1-1-utf-8', 'unicode11utf8', 'unicode20utf8', 'utf8', 'x-unicode20utf8'],
  'utf-16be': ['unicodefffe'],
  'utf-16le': ['csunicode', 'iso-10646-ucs-2', 'ucs-2', 'unicode', 'unicodefeff', 'utf-16'],
  'iso-8859-2': ['iso-ir-101'],
  'iso-8859-3': ['iso-ir-109'],
  'iso-8859-4': ['iso-ir-110'],
  'iso-8859-5': ['csisolatincyrillic', 'cyrillic', 'iso-ir-144'],
  'iso-8859-6': ['arabic', 'asmo-708', 'csiso88596e', 'csiso88596i', 'csisolatinarabic', 'ecma-114', 'iso-8859-6-e', 'iso-8859-6-i', 'iso-ir-127'],
  'iso-8859-7': ['csisolatingreek', 'ecma-118', 'elot_928', 'greek', 'greek8', 'iso-ir-126', 'sun_eu_greek'],
  'iso-8859-8': ['csiso88598e', 'csisolatinhebrew', 'hebrew', 'iso-8859-8-e', 'iso-ir-138', 'visual'],
  'iso-8859-8-i': ['csiso88598i', 'logical'],
  'iso-8859-16': [],
  'koi8-r': ['cskoi8r', 'koi', 'koi8', 'koi8_r'],
  'koi8-u': ['koi8-ru'],
  'windows-874': ['dos-874', 'iso-8859-11', 'iso8859-11', 'iso885911', 'tis-620'],
  ibm866: ['866', 'cp866', 'csibm866'],
  'x-mac-cyrillic': ['x-mac-ukrainian'],
  macintosh: ['csmacintosh', 'mac', 'x-mac-roman'],
  gbk: ['chinese', 'csgb2312', 'csiso58gb231280', 'gb2312', 'gb_2312', 'gb_2312-80', 'iso-ir-58', 'x-gbk'],
  gb18030: [],
  big5: ['big5-hkscs', 'cn-big5', 'csbig5', 'x-x-big5'],
  'euc-jp': ['cseucpkdfmtjapanese', 'x-euc-jp'],
  shift_jis: ['csshiftjis', 'ms932', 'ms_kanji', 'shift-jis', 'sjis', 'windows-31j', 'x-sjis'],
  'euc-kr': ['cseuckr', 'csksc56011987', 'iso-ir-149', 'korean', 'ks_c_5601-1987', 'ks_c_5601-1989', 'ksc5601', 'ksc_5601', 'windows-949'],
  'iso-2022-jp': ['csiso2022jp'],
  replacement: ['csiso2022kr', 'hz-gb-2312', 'iso-2022-cn', 'iso-2022-cn-ext', 'iso-2022-kr'],
  'x-user-defined': [],
}

for (const i of [10, 13, 14, 15]) labels[`iso-8859-${i}`] = [`iso8859-${i}`, `iso8859${i}`]
for (const i of [2, 6, 7]) labels[`iso-8859-${i}`].push(`iso_8859-${i}:1987`)
for (const i of [3, 4, 5, 8]) labels[`iso-8859-${i}`].push(`iso_8859-${i}:1988`)
// prettier-ignore
for (let i = 2; i < 9; i++) labels[`iso-8859-${i}`].push(`iso8859-${i}`, `iso8859${i}`, `iso_8859-${i}`)
for (let i = 2; i < 5; i++) labels[`iso-8859-${i}`].push(`csisolatin${i}`, `l${i}`, `latin${i}`)
for (let i = 0; i < 9; i++) labels[`windows-125${i}`] = [`cp125${i}`, `x-cp125${i}`]

// prettier-ignore
labels['windows-1252'].push('ansi_x3.4-1968', 'ascii', 'cp819', 'csisolatin1', 'ibm819', 'iso-8859-1', 'iso-ir-100', 'iso8859-1', 'iso88591', 'iso_8859-1', 'iso_8859-1:1987', 'l1', 'latin1', 'us-ascii')
// prettier-ignore
labels['windows-1254'].push('csisolatin5', 'iso-8859-9', 'iso-ir-148', 'iso8859-9', 'iso88599', 'iso_8859-9', 'iso_8859-9:1989', 'l5', 'latin5')
labels['iso-8859-10'].push('csisolatin6', 'iso-ir-157', 'l6', 'latin6')
labels['iso-8859-15'].push('csisolatin9', 'iso_8859-15', 'l9')

export default labels
