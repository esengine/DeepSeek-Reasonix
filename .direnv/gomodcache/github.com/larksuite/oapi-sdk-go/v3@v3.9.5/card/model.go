/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package larkcard

import (
	"encoding/json"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

const (
	TemplateBlue      = "blue"
	TemplateWathet    = "wathet"
	TemplateTurquoise = "turquoise"
	TemplateGreen     = "green"
	TemplateYellow    = "yellow"
	TemplateOrange    = "orange"
	TemplateRed       = "red"
	TemplateCarmine   = "carmine"
	TemplateViolet    = "violet"
	TemplatePurple    = "purple"
	TemplateIndigo    = "indigo"
	TemplateGrey      = "grey"
)

type MessageCard struct {
	Config_       *MessageCardConfig       `json:"config,omitempty"`
	Header_       *MessageCardHeader       `json:"header,omitempty"`
	Elements_     []MessageCardElement     `json:"elements,omitempty"`
	I18nElements_ *MessageCardI18nElements `json:"i18n_elements,omitempty"`
	CardLink_     *MessageCardURL          `json:"card_link,omitempty"`
}

func (m *MessageCard) String() (string, error) {
	bs, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}
func NewMessageCard() *MessageCard {
	return &MessageCard{}
}

func (card *MessageCard) Config(config *MessageCardConfig) *MessageCard {
	card.Config_ = config
	return card
}

func (card *MessageCard) Header(header *MessageCardHeader) *MessageCard {
	card.Header_ = header
	return card
}

func (card *MessageCard) Elements(elements []MessageCardElement) *MessageCard {
	card.Elements_ = elements
	return card
}

func (card *MessageCard) I18nElements(i18nElements *MessageCardI18nElements) *MessageCard {
	card.I18nElements_ = i18nElements
	return card
}

func (card *MessageCard) CardLink(cardLink *MessageCardURL) *MessageCard {
	card.CardLink_ = cardLink
	return card
}

func (card *MessageCard) Build() *MessageCard {
	return card
}

type MessageCardI18nElements struct {
	ZhCN_ []MessageCardElement `json:"zh_cn,omitempty"`
	EnUS_ []MessageCardElement `json:"en_us,omitempty"`
	JaJP_ []MessageCardElement `json:"ja_jp,omitempty"`
	ZhHK_ []MessageCardElement `json:"zh_hk,omitempty"`
	ZhTW_ []MessageCardElement `json:"zh_tw,omitempty"`
	IdID_ []MessageCardElement `json:"id_id,omitempty"`
	ViVN_ []MessageCardElement `json:"vi_vn,omitempty"`
	ThTH_ []MessageCardElement `json:"th_th,omitempty"`
	PtBR_ []MessageCardElement `json:"pt_br,omitempty"`
	EsES_ []MessageCardElement `json:"es_es,omitempty"`
	KoKR_ []MessageCardElement `json:"ko_kr,omitempty"`
	DeDE_ []MessageCardElement `json:"de_de,omitempty"`
	FrFR_ []MessageCardElement `json:"fr_fr,omitempty"`
	ItIT_ []MessageCardElement `json:"it_it,omitempty"`
	RuRU_ []MessageCardElement `json:"ru_ru,omitempty"`
	MsMY_ []MessageCardElement `json:"ms_my,omitempty"`
}

func NewMessageCardI18nElements() *MessageCardI18nElements {
	return &MessageCardI18nElements{}
}

func (i18nEle *MessageCardI18nElements) ZhCN(zhCn []MessageCardElement) *MessageCardI18nElements {
	i18nEle.ZhCN_ = zhCn
	return i18nEle
}

func (i18nEle *MessageCardI18nElements) EnUS(enUS []MessageCardElement) *MessageCardI18nElements {
	i18nEle.EnUS_ = enUS
	return i18nEle
}

func (i18nEle *MessageCardI18nElements) JaJP(jaJP []MessageCardElement) *MessageCardI18nElements {
	i18nEle.JaJP_ = jaJP
	return i18nEle
}

func (i18nEle *MessageCardI18nElements) Build() *MessageCardI18nElements {
	return i18nEle
}

type MessageCardElement interface {
	Tag() string
	MarshalJSON() ([]byte, error)
}

type MessageCardActionElement interface {
	MessageCardElement
	IsAction()
}

type MessageCardExtraElement interface {
	MessageCardElement
	IsExtra()
}

type MessageCardNoteElement interface {
	MessageCardElement
	IsNote()
}

type MessageCardHr struct {
}

func NewMessageCardHr() *MessageCardHr {
	return &MessageCardHr{}
}

func (hr *MessageCardHr) Build() *MessageCardHr {
	return hr
}

func (m *MessageCardHr) Tag() string {
	return "hr"
}

func (m *MessageCardHr) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

func messageCardElementJson(e MessageCardElement) ([]byte, error) {
	data, err := larkcore.StructToMap(e)
	if err != nil {
		return nil, err
	}
	data["tag"] = e.Tag()
	return json.Marshal(data)
}

type MessageCardMarkdown struct {
	Content_ string                     `json:"content,omitempty"`
	Href_    map[string]*MessageCardURL `json:"href,omitempty"`
}

func NewMessageCardMarkdown() *MessageCardMarkdown {
	return &MessageCardMarkdown{}
}

func (markDown *MessageCardMarkdown) Content(content string) *MessageCardMarkdown {
	markDown.Content_ = content
	return markDown
}

func (markDown *MessageCardMarkdown) Href(href map[string]*MessageCardURL) *MessageCardMarkdown {
	markDown.Href_ = href
	return markDown
}

func (markDown *MessageCardMarkdown) Build() *MessageCardMarkdown {
	return markDown
}

func (m *MessageCardMarkdown) Tag() string {
	return "markdown"
}

func (m *MessageCardMarkdown) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardDiv struct {
	Text_   MessageCardText         `json:"text,omitempty"`
	Fields_ []*MessageCardField     `json:"fields,omitempty"`
	Extra_  MessageCardExtraElement `json:"extra,omitempty"`
}

func NewMessageCardDiv() *MessageCardDiv {
	return &MessageCardDiv{}
}

func (div *MessageCardDiv) Text(text MessageCardText) *MessageCardDiv {
	div.Text_ = text
	return div
}

func (div *MessageCardDiv) Fields(fields []*MessageCardField) *MessageCardDiv {
	div.Fields_ = fields
	return div
}

func (div *MessageCardDiv) Extra(extra MessageCardExtraElement) *MessageCardDiv {
	div.Extra_ = extra
	return div
}

func (div *MessageCardDiv) Build() *MessageCardDiv {
	return div
}

type MessageCardField struct {
	IsShort_ bool            `json:"is_short,omitempty"`
	Text_    MessageCardText `json:"text,omitempty"`
}

func NewMessageCardField() *MessageCardField {
	return &MessageCardField{}
}

func (field *MessageCardField) IsShort(isShort bool) *MessageCardField {
	field.IsShort_ = isShort
	return field
}

func (field *MessageCardField) Text(text MessageCardText) *MessageCardField {
	field.Text_ = text
	return field
}

func (field *MessageCardField) Build() *MessageCardField {
	return field
}

func (m *MessageCardDiv) Tag() string {
	return "div"
}

func (m *MessageCardDiv) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardText interface {
	MessageCardElement
	Text() string
}
type MessageCardURL struct {
	URL_        string `json:"url,omitempty"`
	AndroidURL_ string `json:"android_url,omitempty"`
	IOSURL_     string `json:"ios_url,omitempty"`
	PCURL_      string `json:"pc_url,omitempty"`
}

func NewMessageCardURL() *MessageCardURL {
	return &MessageCardURL{}
}

func (cardUrl *MessageCardURL) Url(url string) *MessageCardURL {
	cardUrl.URL_ = url
	return cardUrl
}

func (cardUrl *MessageCardURL) AndroidUrl(androidUrl string) *MessageCardURL {
	cardUrl.AndroidURL_ = androidUrl
	return cardUrl
}

func (cardUrl *MessageCardURL) IoSUrl(iOSUrl string) *MessageCardURL {
	cardUrl.IOSURL_ = iOSUrl
	return cardUrl
}

func (cardUrl *MessageCardURL) PcUrl(pcURL string) *MessageCardURL {
	cardUrl.PCURL_ = pcURL
	return cardUrl
}

func (cardUrl *MessageCardURL) Build() *MessageCardURL {
	return cardUrl
}

type MessageCardConfig struct {
	EnableForward_  *bool `json:"enable_forward,omitempty"`
	UpdateMulti_    *bool `json:"update_multi,omitempty"`
	WideScreenMode_ *bool `json:"wide_screen_mode,omitempty"`
}

func NewMessageCardConfig() *MessageCardConfig {
	return &MessageCardConfig{}
}

func (config *MessageCardConfig) EnableForward(enableForward bool) *MessageCardConfig {
	config.EnableForward_ = &enableForward
	return config
}

func (config *MessageCardConfig) UpdateMulti(updateMulti bool) *MessageCardConfig {
	config.UpdateMulti_ = &updateMulti
	return config
}

func (config *MessageCardConfig) WideScreenMode(wideScreenMode bool) *MessageCardConfig {
	config.WideScreenMode_ = &wideScreenMode
	return config
}

func (config *MessageCardConfig) Build() *MessageCardConfig {
	return config
}

type MessageCardHeader struct {
	Template_ *string               `json:"template,omitempty"`
	Title_    *MessageCardPlainText `json:"title,omitempty"`
}

func NewMessageCardHeader() *MessageCardHeader {
	return &MessageCardHeader{}
}

func (header *MessageCardHeader) Template(template string) *MessageCardHeader {
	header.Template_ = &template
	return header
}

func (header *MessageCardHeader) Title(title *MessageCardPlainText) *MessageCardHeader {
	header.Title_ = title
	return header
}

func (header *MessageCardHeader) Build() *MessageCardHeader {
	return header
}

func (m *MessageCard) JSON() (string, error) {
	bs, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}

type MessageCardPlainText struct {
	Content_ string                    `json:"content,omitempty"`
	Lines_   *int                      `json:"lines,omitempty"`
	I18n_    *MessageCardPlainTextI18n `json:"i18n,omitempty"`
}

func NewMessageCardPlainText() *MessageCardPlainText {
	return &MessageCardPlainText{}
}

func (plainText *MessageCardPlainText) Content(content string) *MessageCardPlainText {
	plainText.Content_ = content
	return plainText
}

func (plainText *MessageCardPlainText) Lines(lines int) *MessageCardPlainText {
	plainText.Lines_ = &lines
	return plainText
}

func (plainText *MessageCardPlainText) I18n(i18n *MessageCardPlainTextI18n) *MessageCardPlainText {
	plainText.I18n_ = i18n
	return plainText
}

func (plainText *MessageCardPlainText) Build() *MessageCardPlainText {
	return plainText
}

func (m *MessageCardPlainText) Tag() string {
	return "plain_text"
}

func (m *MessageCardPlainText) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

func (m *MessageCardPlainText) Text() string {
	return m.Content_
}

func (m *MessageCardPlainText) IsExtra() {
}

func (m *MessageCardPlainText) IsNote() {
}

type MessageCardLarkMd struct {
	Content_ string `json:"content,omitempty"`
}

func NewMessageCardLarkMd() *MessageCardLarkMd {
	return &MessageCardLarkMd{}
}

func (md *MessageCardLarkMd) Content(content string) *MessageCardLarkMd {
	md.Content_ = content
	return md
}

func (md *MessageCardLarkMd) Build() *MessageCardLarkMd {
	return md
}

func (m *MessageCardLarkMd) Tag() string {
	return "lark_md"
}

func (m *MessageCardLarkMd) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

func (m *MessageCardLarkMd) Text() string {
	return m.Content_
}

func (m *MessageCardLarkMd) IsExtra() {
}

func (m *MessageCardLarkMd) IsNote() {
}

type MessageCardImageModel string

const (
	MessageCardImageModelFitHorizontal MessageCardImageModel = "fit_horizontal"
	MessageCardImageModelCropCenter    MessageCardImageModel = "crop_center"
)

func (m MessageCardImageModel) Ptr() *MessageCardImageModel {
	return &m
}

type MessageCardImage struct {
	Alt_          *MessageCardPlainText  `json:"alt,omitempty"`
	Title_        MessageCardText        `json:"title,omitempty"`
	ImgKey_       string                 `json:"img_key,omitempty"`
	CustomWidth_  *int                   `json:"custom_width,omitempty"`
	CompactWidth_ *bool                  `json:"compact_width,omitempty"`
	Mode_         *MessageCardImageModel `json:"mode,omitempty"`
	Preview_      *bool                  `json:"preview,omitempty"`
}

func NewMessageCardImage() *MessageCardImage {
	return &MessageCardImage{}
}

func (image *MessageCardImage) Preview(preview bool) *MessageCardImage {
	image.Preview_ = &preview
	return image
}

func (image *MessageCardImage) Alt(alt *MessageCardPlainText) *MessageCardImage {
	image.Alt_ = alt
	return image
}

func (image *MessageCardImage) Title(title MessageCardText) *MessageCardImage {
	image.Title_ = title
	return image
}

func (image *MessageCardImage) ImgKey(imgKey string) *MessageCardImage {
	image.ImgKey_ = imgKey
	return image
}

func (image *MessageCardImage) CustomWidth(customWidth int) *MessageCardImage {
	image.CustomWidth_ = &customWidth
	return image
}

func (image *MessageCardImage) CompactWidth(compactWidth bool) *MessageCardImage {
	image.CompactWidth_ = &compactWidth
	return image
}

func (image *MessageCardImage) Mode(mode MessageCardImageModel) *MessageCardImage {
	image.Mode_ = &mode
	return image
}

func (image *MessageCardImage) Build() *MessageCardImage {
	return image
}

func (m *MessageCardImage) Tag() string {
	return "img"
}

func (m *MessageCardImage) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardNote struct {
	Elements_ []MessageCardNoteElement `json:"elements,omitempty"`
}

func NewMessageCardNote() *MessageCardNote {
	return &MessageCardNote{}
}

func (note *MessageCardNote) Elements(elements []MessageCardNoteElement) *MessageCardNote {
	note.Elements_ = elements
	return note
}

func (note *MessageCardNote) Build() *MessageCardNote {
	return note
}

func (m *MessageCardNote) Tag() string {
	return "note"
}

func (m *MessageCardNote) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardButtonType string

const (
	MessageCardButtonTypeDefault MessageCardButtonType = "default"
	MessageCardButtonTypePrimary MessageCardButtonType = "primary"
	MessageCardButtonTypeDanger  MessageCardButtonType = "danger"
)

func (bt MessageCardButtonType) Ptr() *MessageCardButtonType {
	return &bt
}

type MessageCardActionConfirm struct {
	Title_ *MessageCardPlainText `json:"title,omitempty"`
	Text_  *MessageCardPlainText `json:"text,omitempty"`
}

func NewMessageCardActionConfirm() *MessageCardActionConfirm {
	return &MessageCardActionConfirm{}
}

func (confirm *MessageCardActionConfirm) Title(title *MessageCardPlainText) *MessageCardActionConfirm {
	confirm.Title_ = title
	return confirm
}

func (confirm *MessageCardActionConfirm) Text(text *MessageCardPlainText) *MessageCardActionConfirm {
	confirm.Text_ = text
	return confirm
}

func (confirm *MessageCardActionConfirm) Build() *MessageCardActionConfirm {
	return confirm
}

type MessageCardEmbedImage struct {
	Alt_     *MessageCardPlainText  `json:"alt,omitempty"`
	ImgKey_  string                 `json:"img_key,omitempty"`
	Mode_    *MessageCardImageModel `json:"mode,omitempty"`
	Preview_ *bool                  `json:"preview,omitempty"`
}

func NewMessageCardEmbedImage() *MessageCardEmbedImage {
	return &MessageCardEmbedImage{}
}

func (image *MessageCardEmbedImage) Alt(alt *MessageCardPlainText) *MessageCardEmbedImage {
	image.Alt_ = alt
	return image
}

func (image *MessageCardEmbedImage) ImgKey(imgKey string) *MessageCardEmbedImage {
	image.ImgKey_ = imgKey
	return image
}

func (image *MessageCardEmbedImage) Mode(mode *MessageCardImageModel) *MessageCardEmbedImage {
	image.Mode_ = mode
	return image
}

func (image *MessageCardEmbedImage) Preview(preview bool) *MessageCardEmbedImage {
	image.Preview_ = &preview
	return image
}

func (image *MessageCardEmbedImage) Build() *MessageCardEmbedImage {
	return image
}

func (m *MessageCardEmbedImage) Tag() string {
	return "img"
}

func (m *MessageCardEmbedImage) IsExtra() {
}

func (m *MessageCardEmbedImage) IsNote() {
}

func (m *MessageCardEmbedImage) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardEmbedButton struct {
	Text_     MessageCardText           `json:"text,omitempty"`
	URL_      *string                   `json:"url,omitempty"`
	MultiURL_ *MessageCardURL           `json:"multi_url,omitempty"`
	Type_     *MessageCardButtonType    `json:"type,omitempty"`
	Value_    map[string]interface{}    `json:"value,omitempty"`
	Confirm_  *MessageCardActionConfirm `json:"confirm,omitempty"`
}

func NewMessageCardEmbedButton() *MessageCardEmbedButton {
	return &MessageCardEmbedButton{}
}

func (m *MessageCardEmbedButton) Confirm(confirm *MessageCardActionConfirm) *MessageCardEmbedButton {
	m.Confirm_ = confirm
	return m
}

func (m *MessageCardEmbedButton) Value(value map[string]interface{}) *MessageCardEmbedButton {
	m.Value_ = value
	return m
}

func (m *MessageCardEmbedButton) Type(type_ MessageCardButtonType) *MessageCardEmbedButton {
	m.Type_ = &type_
	return m
}

func (m *MessageCardEmbedButton) Text(text MessageCardText) *MessageCardEmbedButton {
	m.Text_ = text
	return m
}

func (m *MessageCardEmbedButton) Url(url string) *MessageCardEmbedButton {
	m.URL_ = &url
	return m
}

func (m *MessageCardEmbedButton) MultiUrl(multiURL *MessageCardURL) *MessageCardEmbedButton {
	m.MultiURL_ = multiURL
	return m
}
func (m *MessageCardEmbedButton) Build() *MessageCardEmbedButton {
	return m
}

func (m *MessageCardEmbedButton) Tag() string {
	return "button"
}

func (m *MessageCardEmbedButton) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

func (m *MessageCardEmbedButton) IsAction() {
}

func (m *MessageCardEmbedButton) IsExtra() {
}

type MessageCardEmbedDatePickerBase struct {
	InitialDate_     *string                   `json:"initial_date,omitempty"`
	InitialTime_     *string                   `json:"initial_time,omitempty"`
	InitialDatetime_ *string                   `json:"initial_datetime,omitempty"`
	Placeholder_     *MessageCardPlainText     `json:"placeholder,omitempty"`
	Value_           map[string]interface{}    `json:"value,omitempty"`
	Confirm_         *MessageCardActionConfirm `json:"confirm,omitempty"`
}

func NewMessageCardEmbedDatePickerBase() *MessageCardEmbedDatePickerBase {
	return &MessageCardEmbedDatePickerBase{}
}

func (picker *MessageCardEmbedDatePickerBase) InitialDate(initialDate string) *MessageCardEmbedDatePickerBase {
	picker.InitialDate_ = &initialDate
	return picker
}

func (picker *MessageCardEmbedDatePickerBase) InitialTime(initialTime string) *MessageCardEmbedDatePickerBase {
	picker.InitialTime_ = &initialTime
	return picker
}
func (picker *MessageCardEmbedDatePickerBase) InitialDatetime(initialDatetime string) *MessageCardEmbedDatePickerBase {
	picker.InitialDatetime_ = &initialDatetime
	return picker
}
func (picker *MessageCardEmbedDatePickerBase) Placeholder(placeholder *MessageCardPlainText) *MessageCardEmbedDatePickerBase {
	picker.Placeholder_ = placeholder
	return picker
}
func (picker *MessageCardEmbedDatePickerBase) Value(value map[string]interface{}) *MessageCardEmbedDatePickerBase {
	picker.Value_ = value
	return picker
}

func (picker *MessageCardEmbedDatePickerBase) Confirm(confirm *MessageCardActionConfirm) *MessageCardEmbedDatePickerBase {
	picker.Confirm_ = confirm
	return picker
}

func (picker *MessageCardEmbedDatePickerBase) Build() *MessageCardEmbedDatePickerBase {
	return picker
}

func (m *MessageCardEmbedDatePickerBase) IsAction() {
}

func (m *MessageCardEmbedDatePickerBase) IsExtra() {
}

type MessageCardEmbedDatePicker struct {
	*MessageCardEmbedDatePickerBase
}

func NewMessageCardEmbedDatePicker() *MessageCardEmbedDatePicker {
	return &MessageCardEmbedDatePicker{}
}

func (m *MessageCardEmbedDatePicker) MessageCardEmbedDatePicker(base *MessageCardEmbedDatePickerBase) *MessageCardEmbedDatePicker {
	m.MessageCardEmbedDatePickerBase = base
	return m
}

func (m *MessageCardEmbedDatePicker) Tag() string {
	return "date_picker"
}

func (m *MessageCardEmbedDatePicker) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardEmbedPickerTime struct {
	*MessageCardEmbedDatePickerBase
}

func NewMessageCardEmbedPickerTime() *MessageCardEmbedPickerTime {
	return &MessageCardEmbedPickerTime{}
}

func (m *MessageCardEmbedPickerTime) MessageCardEmbedPickerTime(base *MessageCardEmbedDatePickerBase) *MessageCardEmbedPickerTime {
	m.MessageCardEmbedDatePickerBase = base
	return m
}

func (m *MessageCardEmbedPickerTime) Build() *MessageCardEmbedPickerTime {
	return m
}

func (m *MessageCardEmbedPickerTime) Tag() string {
	return "picker_time"
}

func (m *MessageCardEmbedPickerTime) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardEmbedPickerDatetime struct {
	*MessageCardEmbedDatePickerBase
}

func NewMessageCardEmbedPickerDatetime() *MessageCardEmbedPickerDatetime {
	return &MessageCardEmbedPickerDatetime{}
}

func (m *MessageCardEmbedPickerDatetime) MessageCardEmbedPickerDatetime(base *MessageCardEmbedDatePickerBase) *MessageCardEmbedPickerDatetime {
	m.MessageCardEmbedDatePickerBase = base
	return m
}
func (m *MessageCardEmbedPickerDatetime) Build() *MessageCardEmbedPickerDatetime {
	return m
}
func (m *MessageCardEmbedPickerDatetime) Tag() string {
	return "picker_datetime"
}

func (m *MessageCardEmbedPickerDatetime) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardEmbedSelectOption struct {
	Text_     *MessageCardPlainText  `json:"text,omitempty"`
	Value_    string                 `json:"value,omitempty"`
	URL_      *string                `json:"url,omitempty"`
	MultiURL_ *MessageCardURL        `json:"multi_url,omitempty"`
	Type_     *MessageCardButtonType `json:"type,omitempty"`
}

func NewMessageCardEmbedSelectOption() *MessageCardEmbedSelectOption {
	return &MessageCardEmbedSelectOption{}
}

func (m *MessageCardEmbedSelectOption) Text(text *MessageCardPlainText) *MessageCardEmbedSelectOption {
	m.Text_ = text
	return m
}

func (m *MessageCardEmbedSelectOption) Value(value string) *MessageCardEmbedSelectOption {
	m.Value_ = value
	return m
}

func (m *MessageCardEmbedSelectOption) URL(url string) *MessageCardEmbedSelectOption {
	m.URL_ = &url
	return m
}

func (m *MessageCardEmbedSelectOption) MultiUrl(multiUrl *MessageCardURL) *MessageCardEmbedSelectOption {
	m.MultiURL_ = multiUrl
	return m
}

func (m *MessageCardEmbedSelectOption) Type(type_ *MessageCardButtonType) *MessageCardEmbedSelectOption {
	m.Type_ = type_
	return m
}

func (m *MessageCardEmbedSelectOption) Build() *MessageCardEmbedSelectOption {
	return m
}

type MessageCardEmbedOverflow struct {
	Options_ []*MessageCardEmbedSelectOption `json:"options,omitempty"`
	Value_   map[string]interface {
	} `json:"value,omitempty"`
	Confirm_ *MessageCardActionConfirm `json:"confirm,omitempty"`
}

func NewMessageCardEmbedOverflow() *MessageCardEmbedOverflow {
	return &MessageCardEmbedOverflow{}
}

func (overFlow *MessageCardEmbedOverflow) Options(options []*MessageCardEmbedSelectOption) *MessageCardEmbedOverflow {
	overFlow.Options_ = options
	return overFlow
}

func (overFlow *MessageCardEmbedOverflow) Value(value map[string]interface{}) *MessageCardEmbedOverflow {
	overFlow.Value_ = value
	return overFlow
}
func (overFlow *MessageCardEmbedOverflow) Confirm(confirm *MessageCardActionConfirm) *MessageCardEmbedOverflow {
	overFlow.Confirm_ = confirm
	return overFlow
}
func (overFlow *MessageCardEmbedOverflow) Build() *MessageCardEmbedOverflow {
	return overFlow
}

func (m *MessageCardEmbedOverflow) Tag() string {
	return "overflow"
}

func (m *MessageCardEmbedOverflow) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

func (m *MessageCardEmbedOverflow) IsAction() {
}

func (m *MessageCardEmbedOverflow) IsExtra() {
}

type MessageCardEmbedSelectMenuBase struct {
	Placeholder_   *MessageCardPlainText           `json:"placeholder,omitempty"`
	InitialOption_ string                          `json:"initial_option,omitempty"`
	Options_       []*MessageCardEmbedSelectOption `json:"options,omitempty"`
	Value_         map[string]interface {
	} `json:"value,omitempty"`
	Confirm_ *MessageCardActionConfirm `json:"confirm,omitempty"`
}

func NewMessageCardEmbedSelectMenuBase() *MessageCardEmbedSelectMenuBase {
	return &MessageCardEmbedSelectMenuBase{}
}

func (selectMenu *MessageCardEmbedSelectMenuBase) Placeholder(placeholder *MessageCardPlainText) *MessageCardEmbedSelectMenuBase {
	selectMenu.Placeholder_ = placeholder
	return selectMenu
}

func (selectMenu *MessageCardEmbedSelectMenuBase) InitialOption(initialOption string) *MessageCardEmbedSelectMenuBase {
	selectMenu.InitialOption_ = initialOption
	return selectMenu
}

func (selectMenu *MessageCardEmbedSelectMenuBase) Options(options []*MessageCardEmbedSelectOption) *MessageCardEmbedSelectMenuBase {
	selectMenu.Options_ = options
	return selectMenu
}

func (selectMenu *MessageCardEmbedSelectMenuBase) Value(value map[string]interface{}) *MessageCardEmbedSelectMenuBase {
	selectMenu.Value_ = value
	return selectMenu
}

func (selectMenu *MessageCardEmbedSelectMenuBase) Confirm(confirm *MessageCardActionConfirm) *MessageCardEmbedSelectMenuBase {
	selectMenu.Confirm_ = confirm
	return selectMenu
}

func (selectMenu *MessageCardEmbedSelectMenuBase) Build() *MessageCardEmbedSelectMenuBase {
	return selectMenu
}

func (m *MessageCardEmbedSelectMenuBase) IsAction() {
}

func (m *MessageCardEmbedSelectMenuBase) IsExtra() {
}

type MessageCardEmbedSelectMenuStatic struct {
	*MessageCardEmbedSelectMenuBase
}

func NewMessageCardEmbedSelectMenuStatic() *MessageCardEmbedSelectMenuStatic {
	return &MessageCardEmbedSelectMenuStatic{}
}

func (m *MessageCardEmbedSelectMenuStatic) MessageCardEmbedSelectMenuStatic(base *MessageCardEmbedSelectMenuBase) *MessageCardEmbedSelectMenuStatic {
	m.MessageCardEmbedSelectMenuBase = base
	return m
}

func (m *MessageCardEmbedSelectMenuStatic) Build() *MessageCardEmbedSelectMenuStatic {
	return m
}

func (m *MessageCardEmbedSelectMenuStatic) Tag() string {
	return "select_static"
}

func (m *MessageCardEmbedSelectMenuStatic) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardEmbedSelectMenuPerson struct {
	*MessageCardEmbedSelectMenuBase
}

func NewMessageCardEmbedSelectMenuPerson() *MessageCardEmbedSelectMenuPerson {
	return &MessageCardEmbedSelectMenuPerson{}
}

func (menuPerson *MessageCardEmbedSelectMenuPerson) MessageCardEmbedSelectMenu(messageCardEmbedSelectMenuBase *MessageCardEmbedSelectMenuBase) *MessageCardEmbedSelectMenuPerson {
	menuPerson.MessageCardEmbedSelectMenuBase = messageCardEmbedSelectMenuBase
	return menuPerson
}

func (menuPerson *MessageCardEmbedSelectMenuPerson) Build(messageCardEmbedSelectMenuBase *MessageCardEmbedSelectMenuBase) *MessageCardEmbedSelectMenuPerson {
	return menuPerson
}

func (m *MessageCardEmbedSelectMenuPerson) Tag() string {
	return "select_person"
}

func (m *MessageCardEmbedSelectMenuPerson) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardActionLayout string

const (
	MessageCardActionLayoutBisected   MessageCardActionLayout = "bisected"
	MessageCardActionLayoutTrisection MessageCardActionLayout = "trisection"
	MessageCardActionLayoutFlow       MessageCardActionLayout = "flow"
)

func (al MessageCardActionLayout) Ptr() *MessageCardActionLayout {
	return &al
}

type MessageCardAction struct {
	Actions_ []MessageCardActionElement `json:"actions,omitempty"`
	Layout_  *MessageCardActionLayout   `json:"layout,omitempty"`
}

func NewMessageCardAction() *MessageCardAction {
	return &MessageCardAction{}
}

func (cardAction *MessageCardAction) Actions(actions []MessageCardActionElement) *MessageCardAction {
	cardAction.Actions_ = actions
	return cardAction
}

func (cardAction *MessageCardAction) Layout(layout *MessageCardActionLayout) *MessageCardAction {
	cardAction.Layout_ = layout
	return cardAction
}

func (cardAction *MessageCardAction) Build() *MessageCardAction {
	return cardAction
}

func (m *MessageCardAction) Tag() string {
	return "action"
}

func (m *MessageCardAction) MarshalJSON() ([]byte, error) {
	return messageCardElementJson(m)
}

type MessageCardPlainTextI18n struct {
	ZhCN_ string `json:"zh_cn,omitempty"`
	EnUS_ string `json:"en_us,omitempty"`
	JaJP_ string `json:"ja_jp,omitempty"`
	ZhHK_ string `json:"zh_hk,omitempty"`
	ZhTW_ string `json:"zh_tw,omitempty"`
	IdID_ string `json:"id_id,omitempty"`
	ViVN_ string `json:"vi_vn,omitempty"`
	ThTH_ string `json:"th_th,omitempty"`
	PtBR_ string `json:"pt_br,omitempty"`
	EsES_ string `json:"es_es,omitempty"`
	KoKR_ string `json:"ko_kr,omitempty"`
	DeDE_ string `json:"de_de,omitempty"`
	FrFR_ string `json:"fr_fr,omitempty"`
	ItIT_ string `json:"it_it,omitempty"`
	RuRU_ string `json:"ru_ru,omitempty"`
	MsMY_ string `json:"ms_my,omitempty"`
}

func NewMessageCardPlainTextI18n() *MessageCardPlainTextI18n {
	return &MessageCardPlainTextI18n{}
}

func (i18n *MessageCardPlainTextI18n) ZhCN(zhCn string) *MessageCardPlainTextI18n {
	i18n.ZhCN_ = zhCn
	return i18n
}

func (i18n *MessageCardPlainTextI18n) EnUS(enUs string) *MessageCardPlainTextI18n {
	i18n.EnUS_ = enUs
	return i18n
}

func (i18n *MessageCardPlainTextI18n) JaJP(jaJp string) *MessageCardPlainTextI18n {
	i18n.JaJP_ = jaJp
	return i18n
}

func (i18n *MessageCardPlainTextI18n) Build() *MessageCardPlainTextI18n {
	return i18n
}

type CardAction struct {
	*larkevent.EventReq
	OpenID        string `json:"open_id"`
	UserID        string `json:"user_id"`
	OpenMessageID string `json:"open_message_id"`
	OpenChatId    string `json:"open_chat_id"`
	TenantKey     string `json:"tenant_key"`
	Token         string `json:"token"`
	Timezone      string `json:"timezone"`
	Challenge     string `json:"challenge"`
	Type          string `json:"type"`

	Action *struct {
		Value      map[string]interface{} `json:"value"`
		Tag        string                 `json:"tag"`
		Option     string                 `json:"option"`
		Timezone   string                 `json:"timezone"`
		Name       string                 `json:"name"`
		FormValue  map[string]interface{} `json:"form_value"`
		InputValue string                 `json:"input_value"`
		Options    []string               `json:"options"`
		Checked    bool                   `json:"checked"`
	} `json:"action"`
}

type cardChallenge struct {
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	Type      string `json:"type"`
}
type CustomResp struct {
	StatusCode int
	Body       map[string]interface{}
}

type CustomToastBody struct {
	Content string `json:"content"`
	I18n    *I18n  `json:"i18n"`
}

type I18n struct {
	ZhCn string `json:"zh_cn"`
	EnCn string `json:"en_us"`
	JaJp string `json:"ja_jp"`
	ZhHk string `json:"zh_hk"`
	ZhTw string `json:"zh_tw"`
	IdId string `json:"id_id"`
	ViVn string `json:"vi_vn"`
	ThTh string `json:"th_th"`
	PtBr string `json:"pt_br"`
	EsEs string `json:"es_es"`
	KoKr string `json:"ko_kr"`
	DeDe string `json:"de_de"`
	FrFr string `json:"fr_fr"`
	ItIt string `json:"it_it"`
	RuRu string `json:"ru_ru"`
	MsMy string `json:"ms_my"`
}

type CardActionBody struct {
	*CardAction
	Challenge string `json:"challenge"`
	Type      string `json:"type"`
}
