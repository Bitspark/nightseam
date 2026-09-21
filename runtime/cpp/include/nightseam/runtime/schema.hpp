#pragma once

#include <nightseam/runtime/value.hpp>
#include <map>
#include <memory>
#include <string>

namespace nightseam::runtime {

// A descriptor retains the lexical scopes of its imports and explicit
// arguments. Validation throws std::invalid_argument at the first refusal,
// or PublicError("contract_mismatch", ...) for a callable digest mismatch.
class Schema {
public:
    using Imports = std::map<std::string, std::shared_ptr<Schema>>;
    explicit Schema(Value descriptor, std::string digest = {}, Imports imported = {});
    // Bindings are {parameter: {type: expression}} or
    // {parameter: {family: importedName}}. bind also accepts schema objects.
    void validate(const Value& expression, const Value& value,
                  const Value& bindings = Value(jsoncons::json_object_arg),
                  std::string location = "$") const;
    Schema bind(const std::map<std::string, Value>& types, const Imports& families = {}) const;

private:
    struct Data;
    std::shared_ptr<Data> data_;
};

} // namespace nightseam::runtime
