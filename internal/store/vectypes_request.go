package store

// Native vector-store request/response DTOs (companion to vectypes.go). These
// carry the request/point fields and getter chains the SQLite store and its
// callers use — pure in-process Go structs with no server protocol.

// --- point identity ---

// PointId identifies a stored point, either by UUID (the only form used here) or
// a numeric id, with Uuid/Num accessors. The storage key derives from Uuid when
// set, else from Num (see pointIDKey).
type PointId struct {
	Uuid string
	Num  uint64
}

// NewID builds a UUID-based point id.
func NewID(uuid string) *PointId { return &PointId{Uuid: uuid} }

// NewIDNum builds a numeric point id.
func NewIDNum(num uint64) *PointId { return &PointId{Num: num} }

func (p *PointId) GetUuid() string {
	if p == nil {
		return ""
	}
	return p.Uuid
}

func (p *PointId) GetNum() uint64 {
	if p == nil {
		return 0
	}
	return p.Num
}

// --- vectors (input side, set on points to upsert) ---

// DenseVector is a dense float32 vector.
type DenseVector struct {
	Data []float32
}

func (d *DenseVector) GetData() []float32 {
	if d == nil {
		return nil
	}
	return d.Data
}

// Vector is the input vector oneof carrier; only Dense is populated by NewVectors.
type Vector struct {
	Dense *DenseVector
	Data  []float32 // deprecated flat fallback, kept for getter parity
}

func (v *Vector) GetDense() *DenseVector {
	if v == nil {
		return nil
	}
	return v.Dense
}

func (v *Vector) GetData() []float32 {
	if v == nil {
		return nil
	}
	return v.Data
}

// Vectors wraps a single dense Vector on a point being upserted.
type Vectors struct {
	Vector *Vector
}

func (v *Vectors) GetVector() *Vector {
	if v == nil {
		return nil
	}
	return v.Vector
}

// NewVectors builds a dense Vectors instance from float32 values.
func NewVectors(values ...float32) *Vectors {
	return &Vectors{Vector: &Vector{Dense: &DenseVector{Data: values}}}
}

// --- vectors (output side, carried on retrieved/scored points) ---

// VectorOutput is the output vector oneof carrier; Dense is the current form.
type VectorOutput struct {
	Vector isVectorOutputOneof
	Data   []float32 // deprecated flat fallback
}

type isVectorOutputOneof interface{ isVectorOutputOneof() }

type VectorOutput_Dense struct{ Dense *DenseVector }

func (*VectorOutput_Dense) isVectorOutputOneof() {}

func (v *VectorOutput) GetDense() *DenseVector {
	if v == nil {
		return nil
	}
	if d, ok := v.Vector.(*VectorOutput_Dense); ok {
		return d.Dense
	}
	return nil
}

func (v *VectorOutput) GetData() []float32 {
	if v == nil {
		return nil
	}
	return v.Data
}

// VectorsOutput wraps the output vector on a retrieved/scored point.
type VectorsOutput struct {
	VectorsOptions isVectorsOutputOneof
}

type isVectorsOutputOneof interface{ isVectorsOutputOneof() }

type VectorsOutput_Vector struct{ Vector *VectorOutput }

func (*VectorsOutput_Vector) isVectorsOutputOneof() {}

func (v *VectorsOutput) GetVector() *VectorOutput {
	if v == nil {
		return nil
	}
	if o, ok := v.VectorsOptions.(*VectorsOutput_Vector); ok {
		return o.Vector
	}
	return nil
}

// --- query vector input ---

// VectorInput is the query vector; only the dense form is used.
type VectorInput struct {
	Dense *DenseVector
}

func (v *VectorInput) GetDense() *DenseVector {
	if v == nil {
		return nil
	}
	return v.Dense
}

// Query is a nearest-neighbor query carrying the dense query vector.
type Query struct {
	Nearest *VectorInput
}

func (q *Query) GetNearest() *VectorInput {
	if q == nil {
		return nil
	}
	return q.Nearest
}

// NewQuery builds a nearest dense-vector query.
func NewQuery(values ...float32) *Query {
	return &Query{Nearest: &VectorInput{Dense: &DenseVector{Data: values}}}
}

// --- collection config ---

// Distance is the similarity metric; only Cosine is used.
type Distance int32

const Distance_Cosine Distance = 1

// VectorParams configures a collection's dense vector field.
type VectorParams struct {
	Size     uint64
	Distance Distance
}

func (p *VectorParams) GetSize() uint64 {
	if p == nil {
		return 0
	}
	return p.Size
}

// VectorsConfig wraps the dense VectorParams for CreateCollection.
type VectorsConfig struct {
	Params *VectorParams
}

func (c *VectorsConfig) GetParams() *VectorParams {
	if c == nil {
		return nil
	}
	return c.Params
}

// NewVectorsConfig builds a VectorsConfig from dense params.
func NewVectorsConfig(params *VectorParams) *VectorsConfig {
	return &VectorsConfig{Params: params}
}

// --- payload / vector projection selectors ---

// WithPayloadSelector controls payload projection on reads.
type WithPayloadSelector struct {
	SelectorOptions isWithPayloadOneof
}

type isWithPayloadOneof interface{ isWithPayloadOneof() }

type WithPayloadSelector_Enable struct{ Enable bool }
type WithPayloadSelector_Include struct{ Include *PayloadIncludeSelector }
type WithPayloadSelector_Exclude struct{ Exclude *PayloadExcludeSelector }

func (*WithPayloadSelector_Enable) isWithPayloadOneof()  {}
func (*WithPayloadSelector_Include) isWithPayloadOneof() {}
func (*WithPayloadSelector_Exclude) isWithPayloadOneof() {}

func (s *WithPayloadSelector) GetSelectorOptions() isWithPayloadOneof {
	if s == nil {
		return nil
	}
	return s.SelectorOptions
}

type PayloadIncludeSelector struct{ Fields []string }
type PayloadExcludeSelector struct{ Fields []string }

func (s *PayloadIncludeSelector) GetFields() []string {
	if s == nil {
		return nil
	}
	return s.Fields
}

func (s *PayloadExcludeSelector) GetFields() []string {
	if s == nil {
		return nil
	}
	return s.Fields
}

// NewWithPayloadEnable enables (or disables) full-payload inclusion.
func NewWithPayloadEnable(enable bool) *WithPayloadSelector {
	return &WithPayloadSelector{SelectorOptions: &WithPayloadSelector_Enable{Enable: enable}}
}

// NewWithPayloadInclude includes only the named payload fields.
func NewWithPayloadInclude(include ...string) *WithPayloadSelector {
	return &WithPayloadSelector{SelectorOptions: &WithPayloadSelector_Include{Include: &PayloadIncludeSelector{Fields: include}}}
}

// WithVectorsSelector controls vector inclusion on reads.
type WithVectorsSelector struct {
	SelectorOptions isWithVectorsOneof
}

type isWithVectorsOneof interface{ isWithVectorsOneof() }

type WithVectorsSelector_Enable struct{ Enable bool }
type WithVectorsSelector_Include struct{ Include *VectorsIncludeSelector }

func (*WithVectorsSelector_Enable) isWithVectorsOneof()  {}
func (*WithVectorsSelector_Include) isWithVectorsOneof() {}

func (s *WithVectorsSelector) GetSelectorOptions() isWithVectorsOneof {
	if s == nil {
		return nil
	}
	return s.SelectorOptions
}

type VectorsIncludeSelector struct{ Names []string }

func (s *VectorsIncludeSelector) GetNames() []string {
	if s == nil {
		return nil
	}
	return s.Names
}

// NewWithVectorsEnable enables (or disables) vector inclusion on reads.
func NewWithVectorsEnable(enable bool) *WithVectorsSelector {
	return &WithVectorsSelector{SelectorOptions: &WithVectorsSelector_Enable{Enable: enable}}
}

// --- points (records) ---

// PointStruct is an upsert record: id + vectors + payload.
type PointStruct struct {
	Id      *PointId
	Vectors *Vectors
	Payload map[string]*Value
}

func (p *PointStruct) GetId() *PointId {
	if p == nil {
		return nil
	}
	return p.Id
}

func (p *PointStruct) GetVectors() *Vectors {
	if p == nil {
		return nil
	}
	return p.Vectors
}

func (p *PointStruct) GetPayload() map[string]*Value {
	if p == nil {
		return nil
	}
	return p.Payload
}

// ScoredPoint is a query result: id + payload + similarity score (+ vectors).
type ScoredPoint struct {
	Id      *PointId
	Payload map[string]*Value
	Score   float32
	Vectors *VectorsOutput
}

func (p *ScoredPoint) GetId() *PointId {
	if p == nil {
		return nil
	}
	return p.Id
}

func (p *ScoredPoint) GetPayload() map[string]*Value {
	if p == nil {
		return nil
	}
	return p.Payload
}

func (p *ScoredPoint) GetScore() float32 {
	if p == nil {
		return 0
	}
	return p.Score
}

func (p *ScoredPoint) GetVectors() *VectorsOutput {
	if p == nil {
		return nil
	}
	return p.Vectors
}

// RetrievedPoint is a get/scroll result: id + payload (+ vectors).
type RetrievedPoint struct {
	Id      *PointId
	Payload map[string]*Value
	Vectors *VectorsOutput
}

func (p *RetrievedPoint) GetId() *PointId {
	if p == nil {
		return nil
	}
	return p.Id
}

func (p *RetrievedPoint) GetPayload() map[string]*Value {
	if p == nil {
		return nil
	}
	return p.Payload
}

func (p *RetrievedPoint) GetVectors() *VectorsOutput {
	if p == nil {
		return nil
	}
	return p.Vectors
}

// UpdateResult is the write acknowledgement (intentionally empty — callers only
// check the error).
type UpdateResult struct{}

// --- points selector (ids or filter) ---

// PointsSelector selects points by explicit ids or by filter.
type PointsSelector struct {
	Points *PointsIdsList
	Filter *Filter
}

func (s *PointsSelector) GetPoints() *PointsIdsList {
	if s == nil {
		return nil
	}
	return s.Points
}

func (s *PointsSelector) GetFilter() *Filter {
	if s == nil {
		return nil
	}
	return s.Filter
}

// PointsIdsList is a list of explicit point ids.
type PointsIdsList struct {
	Ids []*PointId
}

func (l *PointsIdsList) GetIds() []*PointId {
	if l == nil {
		return nil
	}
	return l.Ids
}

// NewPointsSelector selects points by explicit ids.
func NewPointsSelector(ids ...*PointId) *PointsSelector {
	return &PointsSelector{Points: &PointsIdsList{Ids: ids}}
}

// NewPointsSelectorFilter selects points by filter.
func NewPointsSelectorFilter(filter *Filter) *PointsSelector {
	return &PointsSelector{Filter: filter}
}
