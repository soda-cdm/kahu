# Go E2E Test Framework for Kahu
This document captures high-level design of a Go framework for testing components running on Kahu.  The two overall goals of the framework to support are to allow developers to quickly and easily assemble end-to-end tests and to provide a collection of support packages to help with interacting with the API-server.
<br />
We use Ginkgo framework for Kahu
Ginkgo is a testing framework for Go designed to help you write expressive tests. It is best paired with the Gomega matcher library. When combined, Ginkgo and Gomega provide a rich and expressive DSL (Domain-specific Language) for writing tests.

## Goals
* Properly documented framework to help with adoption
* Easily express end-to-end test and suites using the built-in Go testing package
* Rely and use Go testing package constructs to create test suites and test functions
* Provide helper functions that abstracts Client-Go functionalities
* Support Go test features to easily select/filter tests to run during execution
* Does not specify how tests are built and executed (i.e. e2e.test binary)
* Define optional env/argument variables to control/pass data to tests

## Non-Goals
* Maintain backward compatibility with existing e2e test framework
* Not responsible for bootstrapping or executing the tests themselves (i.e. Ginkgo)
* Initially, this framework will not be responsible for hosting clusters infra components (i.e. Kubetest/Kubetest2)
* Excludes support for fake/mock cluster components for testing

## Design
Suite Environment → setup operations → test steps in testcase function → cleanup operations →Suite environment

## Installing Ginkgo
Ginkgo uses go modules. To add Ginkgo to your project, assuming you have a go.mod file setup, just go install it:
```
go install github.com/onsi/ginkgo/v2/ginkgo
go get github.com/onsi/gomega/...
```
You should now be able to run ginkgo version at the command line and see the Ginkgo CLI emit a version number.

## Using the Framework
### Bootstrapping a Suite
We call a collection of Ginkgo specs in a given package a Ginkgo suite; and we use the word spec to talk about individual Ginkgo tests contained in the suite.
<br />
Ginkgo hooks into Go's existing testing infrastructure. That means that Ginkgo specs live in *_test.go files, just like standard go tests.
In most Ginkgo suites there is only one TestX function - the entry point for Ginkgo. Let's bootstrap a Ginkgo suite to see what that looks like.
Say you have a package named books that you'd like to add a Ginkgo suite to. To bootstrap the suite run:
```
cd path/to/books
ginkgo bootstrap
Generating ginkgo test suite bootstrap for books in:
  Books_suite_test.go
```
This will generate a file named books_suite_test.go in the books directory containing:
```
package books_test

import (
  . "github.com/onsi/ginkgo/v2"
  . "github.com/onsi/gomega"
  "testing"
)

func TestBooks(t *testing.T) {
  RegisterFailHandler(Fail)
  RunSpecs(t, "Books Suite")
}
```
ginkgo bootstrap generates a new test file and places it in the books_test package.
Next we define a single testing test: func TesBooks(t *testing.T). This is the entry point for Ginkgo - the go test runner will run this function when you run go test or ginkgo.
Inside the TestBook function are two lines:
RegisterFailHandler(Fail) is the single line of glue code connecting Ginkgo to Gomega.<br />

Finally the RunSpecs() call tells Ginkgo to start the test suite, passing it the *testing.T instance and a description of the suite. You should only ever call RunSpecs once and you can let Ginkgo worry about calling *testing.T for you.
With the bootstrap file in place, you can now run your suite using the ginkgo command

### Adding Specs to a Suite
While you can add all your specs directly into books_suite_test.go .you'll generally prefer to place your specs in separate files. This is particularly true if you have packages with multiple files that need to be tested. We'd like to test  book.go behavior. We can generate a test file like so:
```
ginkgo generate book
Generating ginkgo test for Book in:
  book_test.go
```
 
This will generate a test file named book_test.go containing:
```
package books_test
 
import (
  . "github.com/onsi/ginkgo/v2"
  . "github.com/onsi/gomega"
 
  "path/to/books"
)
 
var _ = Describe("Books", func() {
 
})
 
```

As with the bootstrapped suite file, this test file is in the separate books_test package and dot-imports both ginkgo and gomega. 
Since we're testing the external interface of book, Ginkgo adds an import statement to pull the books package into the test.

#### Describe()
Ginkgo then adds an empty top-level Describe container node which takes a description and a closure function. Describe("book", func() { }) generates a container that will contain specs that describe the behavior of "Books".
Let's add a few specs, now, to describe our book model's ability to categorize books::
```
var _ = Describe("Books", func() {
  var foxInSocks, lesMis *books.Book

  BeforeEach(func() {
    lesMis = &books.Book{
      Title:  "Les Miserables",
      Author: "Victor Hugo",
      Pages:  2783,
    }

    foxInSocks = &books.Book{
      Title:  "Fox In Socks",
      Author: "Dr. Seuss",
      Pages:  24,
    }
  })

  Describe("Categorizing books", func() {
    Context("with more than 300 pages", func() {
      It("should be a novel", func() {
        Expect(lesMis.Category()).To(Equal(books.CategoryNovel))
      })
    })

    Context("with fewer than 300 pages", func() {
      It("should be a short story", func() {
        Expect(foxInSocks.Category()).To(Equal(books.CategoryShortStory))
      })
    })
  })
})

Assuming a book.Book model with this behavior we can run the tests:
ginkgo
Running Suite: Books Suite - path/to/books
==========================================================
Random Seed: 1634748172

Will run 2 of 2 specs
••

Ran 2 of 2 Specs in 0.000 seconds
SUCCESS! -- 2 Passed | 0 Failed | 0 Pending | 0 Skipped
PASS

Ginkgo ran 1 suite in Xs
Test Suite Passed
```
Success! We've written and run our first Ginkgo suite. From here we can grow our test suite as we iterate on our code.

Now, let us understand the code components used here:
#### Context()
Describe and Context to organize the different aspects of code that we are testing hierarchically. we are describing our book model's ability to categorize books across two different contexts - the behavior for large books "With more than 300 pages" and small books "With fewer than 300 pages".

#### BeforeEach()
BeforeEach is used to set up the state of our specs. In this case,  we are instantiating two new book models: lesMis and foxInSocks

#### It()
 It is to write a spec that makes assertions about the subject under test. In this case,we are ensuring that book.Category() returns the correct category enum based on the length of the book.
Syntax is It(<description>, <closure>)

### Separating Creation and Configuration: JustBeforeEach
 JustBeforeEach nodes run just before the subject node but after any other BeforeEach nodes
It allows you to decouple creation from configuration. Creation occurs in the JustBeforeEach using configuration specified and modified by a chain of BeforeEachs.

### Spec Cleanup: AfterEach and DeferCleanup
Ginkgo also provides setup nodes that run after the spec's subject: AfterEach and JustAfterEach. These are used to clean up after specs .The setup nodes we've seen so far all run before the spec's subject closure each spec.

```
Describe("Reporting book weight", func() {
  var book *books.Book
  var originalWeightUnits string

  BeforeEach(func() {
    book = &books.Book{
      Title: "Les Miserables",
      Author: "Victor Hugo",
      Pages: 2783,
      Weight: 500,
    }
    originalWeightUnits = os.Getenv("WEIGHT_UNITS")
  })

  AfterEach(func() {
    err := os.Setenv("WEIGHT_UNITS", originalWeightUnits)
    Expect(err).NotTo(HaveOccurred())
  })
  ...
})
Now we're guaranteed to clear out WEIGHT_UNITS after each spec as Ginkgo will run the AfterEach node's closure after the subject node for each spec…
And  restore WEIGHT_UNITS to its original value
```
### Suite Setup and Cleanup: BeforeSuite and AfterSuite
Ginkgo supports suite-level setup and cleanup through two specialized suite setup nodes: BeforeSuite and AfterSuite. There can be at most one BeforeSuite node and one AfterSuite node per suite. It is idiomatic to place the suite setup nodes in the Ginkgo bootstrap suite file. <br />

Let's continue to build out our book tests. Books can be stored and retrieved from an external database and we'd like to test this behavior. To do that, we'll need to spin up a database and set up a client to access it. We can do that BeforeEach spec - but doing so would be prohibitively expensive and slow. Instead, it would be more efficient to spin up the database just once when the suite starts. Here's how we'd do it in our books_suite_test.go file:
```
package books_test
import (
  . "github.com/onsi/ginkgo/v2"
  . "github.com/onsi/gomega"
  "path/to/db"
  "testing"
)
var dbRunner *db.Runner
var dbClient *db.Client

func TestBooks(t *testing.T) {
  RegisterFailHandler(Fail)
  RunSpecs(t, "Books Suite")
}
var _ = BeforeSuite(func() {
  dbRunner = db.NewRunner()
  Expect(dbRunner.Start()).To(Succeed())

  dbClient = db.NewClient()
  Expect(dbClient.Connect(dbRunner.Address())).To(Succeed())
})
var _ = AfterSuite(func() {
  Expect(dbRunner.Stop()).To(Succeed())
})
var _ = AfterEach(func() {
   Expect(dbClient.Clear()).To(Succeed())
})
```
Ginkgo will run our BeforeSuite closure at the beginning of the run phase - i.e. after the spec tree has been constructed but before any specs have run. This closure will instantiate a new *db.Runner - this is hypothetical code that knows how to spin up an instance of a database - and ask the runner to Start() a database. <br />

AfterSuite closure will run after all the tests to tear down the running database via dbRunner.Stop(). We can, alternatively, use DeferCleanup to achieve the same effect:
DeferCleanup is context-aware and knows that it's being called in a BeforeSuite. The registered cleanup code will only run after all the specs have completed, just like AfterSuite.

### How Ginkgo Handles Failure
You typically use a matcher library, like Gomega to make assertions in your spec. When a Gomega assertion fails, Gomega generates a failure message and passes it to Ginkgo to signal that the spec has failed. It does this via Ginkgo's global Fail function. Of course, you're allowed to call this function directly yourself
When a failure occurs Ginkgo marks the current spec as failed and moves on to the next spec. If, however, you'd like to stop the entire suite when the first failure occurs you can run ginkgo --fail-fast

### Dot-Importing Ginkgo
Ginkgo users are encouraged to dot-import the Ginkgo DSL into their test suites to effectively extend the Go language with Ginkgo's expressive building blocks:
Like in book_test.go we used 
```
Import . "github.com/onsi/ginkgo"
```
you can choose to dot-import only portions of Ginkgo's DSL into the global namespace. 
```
Import . "github.com/onsi/ginkgo/v2/dsl/core"
```

 To avoid dot-importing dependencies into their code in order to keep their global namespace clean and predictable.
```
import g "github.com/onsi/ginkgo/v2"
```
### Running Specs
Ginkgo assumes specs are independent.
They can be sorted. They can be randomized. They can be filtered. They can be distributed to multiple workers. Ginkgo supports all of these manipulations of the spec list enabling you to randomize, filter, and parallelize your test suite with minimal effort.
To unlock these powerful capabilities Ginkgo makes an important, foundational, assumption about the specs in your suite:

### Spec Randomization
Not yet done

### Spec Parallelization
 This is especially useful when running large, complex, and slow integration suites where the only means to speed things up is to embrace parallelism.
To run a Ginkgo suite in parallel you simply pass the -p flag to ginkgo:
```
ginkgo -p
```
### Filtering Specs
Ginkgo supports all these use cases (and more) through a wide variety of mechanisms to organize and filter specs.

#### Pending Specs
You can mark individual specs, or containers of specs, as Pending. This is used to denote that a spec or its code is under development and should not be run. None of the other filtering mechanisms described in this chapter can override a Pending spec and cause it to run.
Here are all the ways you can mark a spec as Pending:

// With the Pending decorator:
Describe("these specs aren't ready for primetime", Pending, func() { ... })
// By prepending `P` or `X`:
PDescribe("these specs aren't ready for primetime", func() { ... }) <br />

however, run ginkgo --fail-on-pending to have Ginkgo fail the suite if it detects any pending specs. 
Note that pending specs are declared at compile time. You cannot mark a spec as pending dynamically at runtime

#### Skipping Specs
If you need to skip a spec at runtime you can use Ginkgo's Skip(...) function. For example, say we want to skip a spec if some condition is not met. We could:
```
It("should do something, if it can", func() {
  if !someCondition {
    Skip("Special condition wasn't met.")
  }
  ...
})
```
This will cause the current spec to skip. Ginkgo will immediately end execution (Skip, just like Fail, throws a panic to halt execution of the current spec) and mark the spec as skipped. The message passed to Skip will be included in the spec report. Note that Skip does not fail the suite. <br />
You cannot call Skip in a container node(Describe,...) - Skip only applies during the Run Phase, not the Tree Construction Phase.

#### Focused Specs
Ginkgo allows you to Focus individual specs, or containers of specs. When Ginkgo detects focused specs in a suite it skips all other specs and only runs the focused specs.
Here are all the ways you can mark a spec as focused:
// With the Focus decorator:
Describe("just these specs please", Focus, func() { ... })
// By prepending `F`:
FDescribe("just these specs please", func() { ... })
doing so instructs Ginkgo to only run the focused specs. To run all specs, you'll need to go back and remove all the Fs and Focus decorators.
<br />
You can nest focus declarations. Doing so follows a simple rule: if a child node is marked as focused, any of its ancestor nodes that are marked as focused will be unfocused. This behavior was chosen as it most naturally maps onto the developers intent when iterating on a spec suite. For example:
```	
FDescribe("some specs you're debugging", func() {
  It("might be failing", func() { ... })
  It("might also be failing", func() { ... })
})
// With the Focus decorator:
 
Describe("just these specs please", Focus, func() { ... })
 
// By prepending `F`:
 
FDescribe("just these specs please", func() { ... })
```	
 
doing so instructs Ginkgo to only run the focused specs. To run all specs, you'll need to go back and remove all the Fs and Focus decorators.

You can unfocus all specs in a suite by running ginkgo unfocus. This simply strips off any Fs off of FDescribe, FContext, FIt, etc... and removes Focus decorators.

### Spec Labels
Pending, Skip, and Focus provide ad-hoc mechanisms for filtering suites. For particularly large and complex suites, however, you may need a more structured mechanism for organizing and filtering specs. For such usecases, Ginkgo provides labels.
<br />
Labels are simply textual tags that can be attached to Ginkgo container and subject nodes via the Label decorator. Here are the ways you can attach labels to a node:
```	
It("is labelled", Label("first label", "second label"), func() { ... })
It("is labelled", Label("first label"), Label("second label"), func() { ... })
```	
 <br />
Labels can container arbitrary strings but cannot contain any of the characters in the set: "&|!,()/". The labels associated with a spec is the union of all the labels attached to the spec's container nodes and subject nodes. For example:
Describe("Storing books", Label("integration", "storage"), func() {
  It("can save entire shelves of books to the central library", Label("network", "slow", "library storage"), func() {
    // has labels [integration, storage, network, slow, library storage]
  })
You can filter by label using via the ginkgo --label-filter=QUERY flag. 
Example:

Query
Behavior
```
ginkgo --label-filter="integration"
```
Match any specs with the integration label
```
ginkgo --label-filter="!slow"
```
Avoid any specs labelled slow
```
ginkgo --label-filter="network && !slow"
```
Run specs labelled network that aren't slow
```
ginkgo --label-filter=/library/
```	
Run specs with labels matching the regular expression library - this will match the three library-related specs in our example.


list the labels used in a given package using the ginkgo labels

you can also specify suite-wide labels by decorating the RunSpecs command with Label:
```	
func TestBooks(t *testing.T) {
  RegisterFailHandler(Fail)
  RunSpecs(t, "Books Suite", Label("books", "this-is-a-suite-level-label"))
}
``` 
Suite-level labels apply to the entire suite making it easy to filter out entire suites using label filters.

### Location-Based Filtering
Ginkgo allows you to filter specs based on their source code location from the command line. Ginkgo will only run specs that are in files that do match the --focus-file filter and don't match the --skip-file filter. You can provide multiple --focus-file and --skip-file flags.
The argument passed to --focus-file/--skip-file is a file filter and takes one of the following forms:
FILE_REGEX - ginkgo --focus-file=foo will match specs in files like foo_test.go or /foo/bar_test.go.

### Description-Based Filtering
Finally, Ginkgo allows you to filter specs based on the description strings that appear in their subject nodes and/or container hierarchy nodes. You do this using the ginkgo --focus=REGEXP and ginkgo --skip=REGEXP flags.
When --focus and/or --skip are provided Ginkgo will only run specs with descriptions that match the focus regexp and don't match the skip regexp. You can provide --focus and --skip multiple times. The --focus filters will be ORed together and the --skip filters will be ORed together. For example, say you have the following specs:
```	
It("likes dogs", func() {...})
It("likes purple dogs", func() {...})
It("likes cats", func() {...})
It("likes dog fish", func() {...})
It("likes cat fish", func() {...})
It("likes fish", func() {...})
``` 
Then
```
 ginkgo --focus=dog --focus=fish --skip=cat --skip=purple
``` 
will only run "likes dogs", "likes dog fish", and "likes fish".

### Running Specs
By default:
```
ginkgo
 ```

Will run the suite in the current directory. <br />
You can run multiple suites by passing them in as arguments:
ginkgo path/to/suite path/to/other/suite
 
or by running:
```	
ginkgo -r
#or
ginkgo ./...
```
	
which will recurse through the current file tree and run any suites it finds.
To pass additional arguments or custom flags down to your suite use -- to separate your arguments from arguments intended for ginkgo:
```
ginkgo -- <PASS-THROUGHS>
``` 
Finally, note that any Ginkgo flags must appear before the list of packages. Putting it all together:
ginkgo <GINKGO-FLAGS> <PACKAGES> -- <PASS-THROUGHS>
 
By default Ginkgo is running the run subcommand. So all these examples can also be written as ginkgo run <GINKGO-FLAGS> <PACKAGES> -- <PASS-THROUGHS>. 
To get help about Ginkgo's run flags you'll need to run ginkgo help run.

## Resources
https://onsi.github.io/ginkgo/#ginkgo-cli-overview
https://onsi.github.io/gomega/