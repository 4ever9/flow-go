package runtime

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapperlabs/flow-go/language/runtime/errors"
	"github.com/dapperlabs/flow-go/language/runtime/values"
	"github.com/dapperlabs/flow-go/model/flow"
)

type testRuntimeInterface struct {
	resolveImport      func(Location) ([]byte, error)
	getValue           func(controller, owner, key []byte) (value []byte, err error)
	setValue           func(controller, owner, key, value []byte) (err error)
	createAccount      func(publicKeys [][]byte, code []byte) (address flow.Address, err error)
	addAccountKey      func(address flow.Address, publicKey []byte) error
	removeAccountKey   func(address flow.Address, index int) (publicKey []byte, err error)
	updateAccountCode  func(address flow.Address, code []byte) (err error)
	getSigningAccounts func() []flow.Address
	log                func(string)
	emitEvent          func(flow.Event)
}

func (i *testRuntimeInterface) ResolveImport(location Location) ([]byte, error) {
	return i.resolveImport(location)
}

func (i *testRuntimeInterface) GetValue(controller, owner, key []byte) (value []byte, err error) {
	return i.getValue(controller, owner, key)
}

func (i *testRuntimeInterface) SetValue(controller, owner, key, value []byte) (err error) {
	return i.setValue(controller, owner, key, value)
}

func (i *testRuntimeInterface) CreateAccount(publicKeys [][]byte, code []byte) (address flow.Address, err error) {
	return i.createAccount(publicKeys, code)
}

func (i *testRuntimeInterface) AddAccountKey(address flow.Address, publicKey []byte) error {
	return i.addAccountKey(address, publicKey)
}

func (i *testRuntimeInterface) RemoveAccountKey(address flow.Address, index int) (publicKey []byte, err error) {
	return i.removeAccountKey(address, index)
}

func (i *testRuntimeInterface) UpdateAccountCode(address flow.Address, code []byte) (err error) {
	return i.updateAccountCode(address, code)
}

func (i *testRuntimeInterface) GetSigningAccounts() []flow.Address {
	if i.getSigningAccounts == nil {
		return nil
	}
	return i.getSigningAccounts()
}

func (i *testRuntimeInterface) Log(message string) {
	i.log(message)
}

func (i *testRuntimeInterface) EmitEvent(event flow.Event) {
	i.emitEvent(event)
}

func TestRuntimeImport(t *testing.T) {

	runtime := NewInterpreterRuntime()

	importedScript := []byte(`
       fun answer(): Int {
           return 42
		}
	`)

	script := []byte(`
       import "imported"

       fun main(): Int {
           let answer = answer()
           if answer != 42 {
               panic("?!")
           }
           return answer
		}
	`)

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported"):
				return importedScript, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
	}

	value, err := runtime.ExecuteScript(script, runtimeInterface, nil)
	assert.Nil(t, err)
	assert.Equal(t, values.Int(42), value)
}

func TestRuntimeInvalidMainMissingAccount(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script := []byte(`
       fun main(): Int {
           return 42
		}
	`)

	runtimeInterface := &testRuntimeInterface{
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
	}

	_, err := runtime.ExecuteScript(script, runtimeInterface, nil)
	assert.Error(t, err)
}

func TestRuntimeMainWithAccount(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script := []byte(`
       fun main(account: Account): Int {
           log(account.address)
           return 42
		}
	`)

	var loggedMessage string

	runtimeInterface := &testRuntimeInterface{
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return nil, nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessage = message
		},
	}

	value, err := runtime.ExecuteScript(script, runtimeInterface, nil)

	assert.Nil(t, err)
	assert.Equal(t, values.Int(42), value)
	assert.Equal(t, `"2a00000000000000000000000000000000000000"`, loggedMessage)
}

func TestRuntimeStorage(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script := []byte(`
       fun main(account: Account) {
           log(account.storage[Int])

           account.storage[Int] = 42
           log(account.storage[Int])

           account.storage[[Int]] = [1, 2, 3]
           log(account.storage[[Int]])

           account.storage[String] = "xyz"
           log(account.storage[String])
       }
	`)

	storedValues := map[string][]byte{}

	var loggedMessages []string

	runtimeInterface := &testRuntimeInterface{
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValues[string(key)], nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValues[string(key)] = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script, runtimeInterface, nil)

	require.Nil(t, err)

	assert.Equal(t, []string{"nil", "42", "[1, 2, 3]", `"xyz"`}, loggedMessages)
}

func TestRuntimeStorageMultipleTransactions(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script := []byte(`
       fun main(account: Account) {
           log(account.storage[[String]])
           account.storage[[String]] = ["A", "B"]
       }
	`)

	var loggedMessages []string
	var storedValue []byte

	runtimeInterface := &testRuntimeInterface{
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValue, nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValue = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script, runtimeInterface, nil)
	assert.Nil(t, err)

	_, err = runtime.ExecuteScript(script, runtimeInterface, nil)
	assert.Nil(t, err)

	assert.Equal(t, []string{"nil", `["A", "B"]`}, loggedMessages)
}

// TestRuntimeStorageMultipleTransactionsStructures tests a function call
// of a stored structure declared in an imported program
//
func TestRuntimeStorageMultipleTransactionsStructures(t *testing.T) {

	runtime := NewInterpreterRuntime()

	deepThought := []byte(`
       struct DeepThought {
           fun answer(): Int {
               return 42
           }
       }
	`)

	script1 := []byte(`
	   import "deep-thought"

       fun main(account: Account) {
           account.storage[DeepThought] = DeepThought()

           log(account.storage[DeepThought])
       }
	`)

	script2 := []byte(`
	   import "deep-thought"

       fun main(account: Account): Int {
           log(account.storage[DeepThought])

           let computer = account.storage[DeepThought]
               ?? panic("missing computer")

           return computer.answer()
       }
	`)

	var loggedMessages []string
	var storedValue []byte

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("deep-thought"):
				return deepThought, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValue, nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValue = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	assert.Nil(t, err)

	answer, err := runtime.ExecuteScript(script2, runtimeInterface, nil)
	assert.Nil(t, err)
	assert.Equal(t, values.Int(42), answer)
}

func TestRuntimeStorageMultipleTransactionsInt(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script1 := []byte(`
	  fun main(account: Account) {
	      account.storage[Int] = 42
	  }
	`)

	script2 := []byte(`
	  fun main(account: Account): Int {
	      return account.storage[Int] ?? panic("stored value is nil")
	  }
	`)

	var loggedMessages []string
	var storedValue []byte

	runtimeInterface := &testRuntimeInterface{
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValue, nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValue = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	assert.Nil(t, err)

	result, err := runtime.ExecuteScript(script2, runtimeInterface, nil)
	assert.Equal(t, values.Int(42), result)
	assert.Nil(t, err)
}

// TestRuntimeCompositeFunctionInvocationFromImportingProgram checks
// that member functions of imported composites can be invoked from an importing program.
// See https://github.com/dapperlabs/flow-go/issues/838
//
func TestRuntimeCompositeFunctionInvocationFromImportingProgram(t *testing.T) {

	runtime := NewInterpreterRuntime()

	imported := []byte(`
      // function must have arguments
      fun x(x: Int) {}

      // invocation must be in composite
      struct Y {
        fun x() {
          x(x: 1)
        }
      }
    `)

	script1 := []byte(`
      import Y from "imported"

      fun main(account: Account) {
	      account.storage[Y] = Y()
	  }
    `)

	script2 := []byte(`
      import Y from "imported"

      fun main(account: Account) {
          let y = account.storage[Y] ?? panic("stored value is nil")
          y.x()
      }
    `)

	var storedValue []byte

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported"):
				return imported, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValue, nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValue = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	assert.Nil(t, err)

	_, err = runtime.ExecuteScript(script2, runtimeInterface, nil)
	assert.Nil(t, err)
}

func TestRuntimeResourceContractUseThroughReference(t *testing.T) {

	runtime := NewInterpreterRuntime()

	imported := []byte(`
      resource R {
        fun x() {
          log("x!")
        }
      }

      fun createR(): <-R {
          return <- create R()
      }
    `)

	script1 := []byte(`
      import R, createR from "imported"

      fun main(account: Account) {
          var r: <-R? <- createR()
	      account.storage[R] <-> r
          if r != nil {
             panic("already initialized")
          }
          destroy r
	  }
    `)

	script2 := []byte(`
      import R from "imported"

      fun main(account: Account) {
          let ref = &account.storage[R] as R
          ref.x()
      }
    `)

	storedValues := map[string][]byte{}

	var loggedMessages []string

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported"):
				return imported, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValues[string(key)], nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValues[string(key)] = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	_, err = runtime.ExecuteScript(script2, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	assert.Equal(t, []string{"\"x!\""}, loggedMessages)
}

func TestRuntimeResourceContractUseThroughStoredReference(t *testing.T) {

	runtime := NewInterpreterRuntime()

	imported := []byte(`
      resource R {
        fun x() {
          log("x!")
        }
      }

      fun createR(): <-R {
          return <- create R()
      }
    `)

	script1 := []byte(`
      import R, createR from "imported"

      fun main(account: Account) {
          var r: <-R? <- createR()
	      account.storage[R] <-> r
          if r != nil {
             panic("already initialized")
          }
          destroy r

          account.storage[&R] = &account.storage[R] as R
	  }
    `)

	script2 := []byte(`
	 import R from "imported"

	 fun main(account: Account) {
	     let ref = account.storage[&R] ?? panic("no R ref")
	     ref.x()
	 }
	`)

	storedValues := map[string][]byte{}

	var loggedMessages []string

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported"):
				return imported, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValues[string(key)], nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValues[string(key)] = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	_, err = runtime.ExecuteScript(script2, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	assert.Equal(t, []string{"\"x!\""}, loggedMessages)
}

func TestRuntimeResourceContractWithInterface(t *testing.T) {

	runtime := NewInterpreterRuntime()

	imported1 := []byte(`
      resource interface RI {
        fun x()
      }
    `)

	imported2 := []byte(`
      import RI from "imported1"

      resource R: RI {
        fun x() {
          log("x!")
        }
      }

      fun createR(): <-R {
          return <- create R()
      }
    `)

	script1 := []byte(`
	  import RI from "imported1"
      import R, createR from "imported2"

      fun main(account: Account) {
          var r: <-R? <- createR()
	      account.storage[R] <-> r
          if r != nil {
             panic("already initialized")
          }
          destroy r

          account.storage[&RI] = &account.storage[R] as RI
	  }
    `)

	// TODO: Get rid of the requirement that the underlying type must be imported.
	//   This requires properly initializing Interpreter.CompositeFunctions.
	//   Also initialize Interpreter.DestructorFunctions

	script2 := []byte(`
	  import RI from "imported1"
      import R from "imported2"

	  fun main(account: Account) {
	      let ref = account.storage[&RI] ?? panic("no RI ref")
	      ref.x()
	  }
	`)

	storedValues := map[string][]byte{}

	var loggedMessages []string

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported1"):
				return imported1, nil
			case StringLocation("imported2"):
				return imported2, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValues[string(key)], nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValues[string(key)] = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	_, err = runtime.ExecuteScript(script2, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	assert.Equal(t, []string{"\"x!\""}, loggedMessages)
}

func TestParseAndCheckProgram(t *testing.T) {
	t.Run("ValidProgram", func(t *testing.T) {
		runtime := NewInterpreterRuntime()

		script := []byte("fun test(): Int { return 42 }")
		runtimeInterface := &testRuntimeInterface{}

		err := runtime.ParseAndCheckProgram(script, runtimeInterface, nil)
		assert.Nil(t, err)
	})

	t.Run("InvalidSyntax", func(t *testing.T) {
		runtime := NewInterpreterRuntime()

		script := []byte("invalid syntax")
		runtimeInterface := &testRuntimeInterface{}

		err := runtime.ParseAndCheckProgram(script, runtimeInterface, nil)
		assert.NotNil(t, err)
	})

	t.Run("InvalidSemantics", func(t *testing.T) {
		runtime := NewInterpreterRuntime()

		script := []byte(`let a: Int = "b"`)
		runtimeInterface := &testRuntimeInterface{}

		err := runtime.ParseAndCheckProgram(script, runtimeInterface, nil)
		assert.NotNil(t, err)
	})
}

func TestRuntimeSyntaxError(t *testing.T) {

	runtime := NewInterpreterRuntime()

	script := []byte(`
      fun main(account: Account): String {
          return "Hello World!
      }
	`)

	runtimeInterface := &testRuntimeInterface{
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
	}

	_, err := runtime.ExecuteScript(script, runtimeInterface, nil)

	assert.Error(t, err)
}

func TestRuntimeStorageChanges(t *testing.T) {

	runtime := NewInterpreterRuntime()

	imported := []byte(`
      resource X {
          var x: Int
          init() {
              self.x = 0
          }
      }

      fun createX(): <-X {
          return <-create X()
      }
    `)

	script1 := []byte(`
	  import X, createX from "imported"

      fun main(account: Account) {
          var x: <-X? <- createX()
          account.storage[X] <-> x
          destroy x

          let ref = &account.storage[X] as X
          ref.x = 1
	  }
    `)

	script2 := []byte(`
	  import X from "imported"

	  fun main(account: Account) {
	      let ref = &account.storage[X] as X
          log(ref.x)
	  }
	`)

	storedValues := map[string][]byte{}

	var loggedMessages []string

	runtimeInterface := &testRuntimeInterface{
		resolveImport: func(location Location) (bytes []byte, err error) {
			switch location {
			case StringLocation("imported"):
				return imported, nil
			default:
				return nil, fmt.Errorf("unknown import location: %s", location)
			}
		},
		getValue: func(controller, owner, key []byte) (value []byte, err error) {
			return storedValues[string(key)], nil
		},
		setValue: func(controller, owner, key, value []byte) (err error) {
			storedValues[string(key)] = value
			return nil
		},
		getSigningAccounts: func() []flow.Address {
			return []flow.Address{[20]byte{42}}
		},
		log: func(message string) {
			loggedMessages = append(loggedMessages, message)
		},
	}

	_, err := runtime.ExecuteScript(script1, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	_, err = runtime.ExecuteScript(script2, runtimeInterface, nil)
	if !assert.Nil(t, err) {
		assert.FailNow(t, errors.UnrollChildErrors(err))
	}

	assert.Equal(t, []string{"1"}, loggedMessages)
}
